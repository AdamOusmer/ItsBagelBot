# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.CacheTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.TestSupport.HttpClient

  @resolve "rpc.internal.projector.overlays.resolve"
  @token Base.url_encode64(:binary.copy(<<71>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<72>>, 32), padding: false)
  @overlay %{
    user_id: 9,
    overlay_id: 13,
    kind: "system",
    template: "powered",
    revision: 1,
    generation: 2,
    allowed: true,
    premium: false,
    document: %{"template" => "powered", "config" => %{}}
  }

  setup do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))
    on_exit(fn -> Application.put_env(:controls_gateway, :overlay_cookie_key, key) end)

    projector = start_supervised!({Agent, fn -> %{mode: :up, calls: 0} end})
    responder = start_supervised!({Task, fn -> respond(projector) end})
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @resolve)
    %{projector: projector}
  end

  test "repeated exchanges of a dead link cost one projector request", %{
    http_port: port,
    projector: projector
  } do
    for _ <- 1..5, do: assert(%{status: 404} = exchange(port, @unknown))
    assert calls(projector) == 1
  end

  test "an invalidation naming the token hash sends the next exchange to the projector", %{
    http_port: port,
    projector: projector
  } do
    assert %{status: 404} = exchange(port, @unknown)
    assert calls(projector) == 1

    eventually(fn ->
      :ok = Gnat.pub(:gnat_rpc, "cache.invalidate.overlays", invalidation(@unknown))
      exchange(port, @unknown)
      calls(projector) == 2
    end)

    exchange(port, @unknown)
    assert calls(projector) == 2
  end

  @tag cache_ttl_ms: 100
  test "a stale entry keeps serving the document and the stream while the projector is down", %{
    http_port: port,
    projector: projector
  } do
    cookie = cookie(port)
    live = events(port, cookie)
    Process.sleep(200)
    Agent.update(projector, &%{&1 | mode: :down})
    before = calls(projector)

    assert %{status: 200} = HttpClient.request(port, "GET", "/o/doc", headers: [cookie])
    assert calls(projector) > before

    Registry.select(ControlsGateway.Connections, [{{:_, :"$1", :_}, [], [:"$1"]}])
    |> Enum.each(&send(&1, :check))

    refute HttpClient.drain(live, 300).done
    HttpClient.close(live)
  end

  defp events(port, cookie) do
    port
    |> HttpClient.open("GET", "/o/events", headers: [cookie])
    |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))
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

  defp invalidation(token) do
    Jason.encode!(%{broadcaster_id: "9", keys: [sha(token)]})
  end

  defp calls(projector), do: Agent.get(projector, & &1.calls)

  defp eventually(fun, attempts \\ 40) do
    cond do
      fun.() ->
        :ok

      attempts == 0 ->
        flunk("condition not met")

      true ->
        Process.sleep(50)
        eventually(fun, attempts - 1)
    end
  end

  defp respond(projector) do
    receive do
      {:msg, %{reply_to: reply, body: body}} ->
        mode = Agent.get_and_update(projector, &{&1.mode, %{&1 | calls: &1.calls + 1}})
        payload = response(Jason.decode!(body), mode)
        :ok = Gnat.pub(:gnat_rpc, reply, Jason.encode!(payload))
        respond(projector)
    end
  end

  defp response(%{"token_hash" => hash}, mode) do
    cond do
      hash != sha(@token) -> %{error: "overlay not found", code: "not_found"}
      mode == :down -> %{error: "projection down", code: "unavailable"}
      true -> @overlay
    end
  end

  defp sha(token), do: :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)
end
