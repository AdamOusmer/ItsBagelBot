# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.TestSupport.HttpClient

  @resolve "rpc.internal.projector.overlays.resolve"
  @token Base.url_encode64(:binary.copy(<<61>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<62>>, 32), padding: false)
  @broken Base.url_encode64(:binary.copy(<<63>>, 32), padding: false)
  @document %{
    "version" => 2,
    "kind" => "system",
    "template" => "powered",
    "config" => %{"every_minutes" => 15, "show_seconds" => 8}
  }
  @overlay %{
    user_id: 7,
    overlay_id: 11,
    kind: "system",
    template: "powered",
    revision: 3,
    generation: 5,
    allowed: true,
    document: @document
  }

  setup tags do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    checks = Application.get_env(:controls_gateway, :overlay_check_ms)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))
    Application.put_env(:controls_gateway, :overlay_check_ms, 50)

    on_exit(fn ->
      Application.put_env(:controls_gateway, :overlay_cookie_key, key)
      Application.put_env(:controls_gateway, :overlay_check_ms, checks)
    end)

    overlay = Map.merge(@overlay, tags[:overlay] || %{})
    agent = start_supervised!({Agent, fn -> overlay end})
    responder = start_supervised!({Task, fn -> respond(agent) end})
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @resolve)
    %{agent: agent}
  end

  test "the overlay page is the powered page with the same policy", %{http_port: port} do
    assert %{status: 200, headers: %{"content-security-policy" => csp}, body: body} =
             HttpClient.request(port, "GET", "/o")

    assert %{body: ^body, headers: %{"content-security-policy" => ^csp}} =
             HttpClient.request(port, "GET", "/powered")

    assert csp =~ "frame-src https://widgets.itsbagelbot.com"
  end

  test "wrong methods are refused with the allowed method", %{http_port: port} do
    for {method, path, allow} <- [
          {"POST", "/o", "GET"},
          {"GET", "/o/session", "POST"},
          {"POST", "/o/doc", "GET"},
          {"POST", "/o/events", "GET"}
        ] do
      assert %{status: 405, headers: %{"allow" => ^allow}} =
               HttpClient.request(port, method, path)
    end
  end

  test "session is signed and scoped to the overlay path", %{http_port: port} do
    assert %{status: 204, headers: %{"set-cookie" => set_cookie}} = exchange(port, @token)

    for attribute <- ["bb_o=", "HttpOnly", "Secure", "SameSite=Lax", "Path=/o", "Max-Age=2592000"] do
      assert String.downcase(set_cookie) =~ String.downcase(attribute)
    end

    refute String.downcase(set_cookie) =~ "domain="

    for path <- ["/o/doc", "/o/events"] do
      assert %{status: 401} =
               HttpClient.request(port, "GET", path, headers: [{"cookie", "bb_o=forged"}])
    end
  end

  test "unknown tokens are 404, refusals are 503 and tokens never reach the logs", %{
    http_port: port
  } do
    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        assert %{status: 404} = exchange(port, @unknown)
        assert %{status: 503} = exchange(port, @broken)
      end)

    for token <- [@unknown, @broken, @token], do: refute(log =~ token)
  end

  test "a system overlay is allowed for a free account", %{http_port: port} do
    assert %{status: 200, headers: headers, body: body} =
             HttpClient.request(port, "GET", "/o/doc", headers: [cookie(port)])

    assert headers["cache-control"] == "no-store"

    assert Jason.decode!(body) == %{
             "document" => @document,
             "revision" => 3,
             "premium" => false
           }
  end

  @tag overlay: %{kind: "custom", allowed: false, document: nil}
  test "a custom overlay without premium opens a session and is refused with a reason", %{
    http_port: port
  } do
    assert %{status: 403, body: body} =
             HttpClient.request(port, "GET", "/o/doc", headers: [cookie(port)])

    assert Jason.decode!(body) == %{"reason" => "premium_required"}
  end

  test "expired sessions are 401", %{http_port: port} do
    expired = claims(%{"exp" => System.system_time(:second) - 10})
    assert %{status: 401} = HttpClient.request(port, "GET", "/o/doc", headers: [signed(expired)])

    fresh = claims(%{"exp" => System.system_time(:second) + 60})
    assert %{status: 200} = HttpClient.request(port, "GET", "/o/doc", headers: [signed(fresh)])
  end

  test "a rotated link or generation is refused as not found", %{http_port: port, agent: agent} do
    headers = [cookie(port)]
    Agent.update(agent, &Map.put(&1, :generation, 6))
    assert doc_reason(port, headers) == "not_found"

    Agent.update(agent, &Map.put(&1, :gone, true))
    assert doc_reason(port, headers) == "not_found"
  end

  test "events open for a valid session and announce revisions", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    Agent.update(agent, &Map.put(&1, :revision, 4))
    data = HttpClient.await(live, &String.contains?(&1.body, "event: revision\ndata: 4\n\n"))
    HttpClient.close(data)
  end

  test "events close when the generation changes", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    Agent.update(agent, &Map.put(&1, :generation, 6))
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  test "events close when the overlay stops being allowed", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    Agent.update(agent, &Map.merge(&1, %{kind: "custom", allowed: false, document: nil}))
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  @tag overlay: %{kind: "custom", allowed: false, document: nil}
  test "events end with no content when the overlay is not allowed", %{http_port: port} do
    assert %{status: 204, body: ""} =
             HttpClient.request(port, "GET", "/o/events", headers: [cookie(port)])
  end

  defp events(port, headers) do
    response =
      port
      |> HttpClient.open("GET", "/o/events", headers: headers)
      |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))

    assert %{status: 200, headers: %{"content-type" => "text/event-stream"}, done: false} =
             response

    response
  end

  defp doc_reason(port, headers) do
    assert %{status: 403, body: body} =
             HttpClient.request(port, "GET", "/o/doc", headers: headers)

    Jason.decode!(body)["reason"]
  end

  defp exchange(port, token) do
    HttpClient.request(port, "POST", "/o/session",
      headers: [{"content-type", "application/json"}],
      body: Jason.encode!(%{token: token})
    )
  end

  defp cookie(port) do
    %{status: 204, headers: %{"set-cookie" => set_cookie}} = exchange(port, @token)
    {"cookie", set_cookie |> String.split(";", parts: 2) |> hd()}
  end

  defp claims(overrides) do
    Map.merge(
      %{
        "overlay_id" => 11,
        "generation" => 5,
        "token_hash" => :crypto.hash(:sha256, @token) |> Base.encode16(case: :lower)
      },
      overrides
    )
  end

  defp signed(claims) do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    payload = claims |> Jason.encode!() |> Base.url_encode64(padding: false)
    mac = :crypto.mac(:hmac, :sha256, key, payload) |> Base.url_encode64(padding: false)
    {"cookie", "bb_o=" <> payload <> "." <> mac}
  end

  defp respond(agent) do
    receive do
      {:msg, %{reply_to: reply, body: body}} ->
        payload = response(Jason.decode!(body), Agent.get(agent, & &1))
        :ok = Gnat.pub(:gnat_rpc, reply, Jason.encode!(payload))
        respond(agent)
    end
  end

  defp response(%{"token_hash" => hash}, overlay) do
    cond do
      hash == sha(@broken) -> %{error: "projection down", code: "unavailable"}
      hash == sha(@token) and not Map.has_key?(overlay, :gone) -> reject_nil(overlay)
      true -> %{error: "overlay not found", code: "not_found"}
    end
  end

  defp reject_nil(overlay), do: Map.reject(overlay, fn {_key, value} -> is_nil(value) end)

  defp sha(token), do: :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)
end
