# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TtsTest do
  use ControlsGateway.IntegrationCase, async: false
  import ExUnit.CaptureLog

  alias ControlsGateway.TestSupport.HttpClient
  alias Gnat.Jetstream.API.{Object, Stream}

  @moduletag :tmp_dir
  @moduletag jetstream: true

  @user_id 18_446_744_073_709_551_615
  @generation 7
  @overlay "rpc.internal.users.voice.overlay."
  @token Base.url_encode64(:binary.copy(<<33>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<34>>, 32), padding: false)
  @ttl_ns 300_000_000_000

  setup tags do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))

    on_exit(fn ->
      Application.put_env(:controls_gateway, :overlay_cookie_key, key)
    end)

    known_hash = :crypto.hash(:sha256, @token) |> Base.encode16(case: :lower)
    responder = start_supervised!({Task, fn -> respond(known_hash) end})
    {:ok, _} = Gnat.sub(:gnat, responder, @overlay <> "resolve")
    {:ok, _} = Gnat.sub(:gnat, responder, @overlay <> "check")

    if !tags[:no_streams] do
      {:ok, _} =
        Stream.create(:gnat, %Stream{
          name: "OVERLAY_TTS",
          subjects: ["overlay.tts.*"],
          storage: :file,
          max_age: @ttl_ns
        })

      {:ok, _} = Object.create_bucket(:gnat, "TTS_CLIPS", storage: :file, ttl: @ttl_ns)
    end

    :ok
  end

  test "only the beta routes are mounted and the overlay page sends a restrictive policy", %{
    http_port: port
  } do
    for path <- ["/v1/connect", "/metrics", "/bridge/v1/events"] do
      assert %{status: 404} = HttpClient.request(port, "GET", path)
    end

    assert %{status: 200, headers: headers, body: body} = HttpClient.request(port, "GET", "/tts")
    assert headers["referrer-policy"] == "no-referrer"
    assert headers["content-security-policy"] =~ "default-src 'none'"
    assert body =~ "location.hash"
  end

  test "unknown tokens are 404 and never appear in logs", %{http_port: port} do
    log =
      capture_log([level: :debug], fn ->
        assert %{status: 404, headers: %{"cache-control" => "no-store"}} =
                 exchange(port, @unknown)

        assert %{status: 404} =
                 HttpClient.request(port, "GET", "/tts/clip/#{@unknown}?token=#{@unknown}",
                   headers: [cookie(port)]
                 )
      end)

    assert log =~ "TTS session rejected"
    refute log =~ @unknown
    refute log =~ @token
  end

  test "session is signed and scoped to the overlay host and path", %{http_port: port} do
    assert %{status: 204, headers: %{"set-cookie" => set_cookie}} = exchange(port, @token)

    for attribute <- [
          "bb_tts=",
          "HttpOnly",
          "Secure",
          "SameSite=Lax",
          "Path=/tts",
          "Max-Age=2592000"
        ] do
      assert String.downcase(set_cookie) =~ String.downcase(attribute)
    end

    refute String.downcase(set_cookie) =~ "domain="

    assert %{status: 401} =
             HttpClient.request(port, "GET", "/tts/events",
               headers: [{"cookie", "bb_tts=forged"}]
             )
  end

  test "failed attempts are throttled per client IP", %{http_port: port} do
    setting = :overlay_trust_cloudflare_ip
    original = Application.get_env(:controls_gateway, setting)
    Application.put_env(:controls_gateway, setting, true)
    on_exit(fn -> Application.put_env(:controls_gateway, setting, original) end)

    ip = {"cf-connecting-ip", "192.0.2.1"}

    for _ <- 1..10 do
      assert %{status: 404} = exchange(port, @unknown, [ip])
    end

    assert %{status: 429} = exchange(port, @token, [ip])
    assert %{status: 204} = exchange(port, @token, [{"cf-connecting-ip", "192.0.2.2"}])
  end

  test "SSE sends new clips in order and resumes after a stream sequence", %{http_port: port} do
    cookie = cookie(port)
    old = publish_clip("old")
    live = events(port, [cookie])
    other = publish_clip("different-account", 42)
    first = publish_clip("first")

    data = HttpClient.await(live, &String.contains?(&1.body, "event_id"))
    assert data.body =~ "id: #{first}\ndata: "
    refute data.body =~ "id: #{other}\n"
    refute data.body =~ "id: #{old}\n"
    HttpClient.close(data)

    next = publish_clip("second")
    resumed = events(port, [cookie, {"last-event-id", Integer.to_string(first)}])
    data = HttpClient.await(resumed, &String.contains?(&1.body, "event_id"))
    assert data.body =~ "id: #{next}\ndata: "
    refute data.body =~ "id: #{first}\n"
    HttpClient.close(data)
  end

  test "clip reads stream multiple chunks and isolate accounts", %{http_port: port} do
    audio = "RIFF" <> :binary.copy(<<1, 2, 3, 4>>, 50_000)
    upload("#{@user_id}/clip", audio)
    upload("42/foreign", audio)
    headers = [cookie(port)]

    assert %{status: 200, body: ^audio, headers: response_headers} =
             HttpClient.request(port, "GET", "/tts/clip/clip", headers: headers)

    assert response_headers["content-type"] == "audio/wav"
    assert response_headers["cache-control"] == "no-store"
    assert %{status: 404} = HttpClient.request(port, "GET", "/tts/clip/foreign", headers: headers)
  end

  test "revocation closes an open event stream", %{http_port: port} do
    live = events(port, [cookie(port)])
    :ok = Gnat.pub(:gnat, "controls.tts.revoked.#{@user_id}", "")
    assert HttpClient.await(live, & &1.done).done
    HttpClient.close(live)
  end

  @tag no_streams: true
  test "missing JetStream resources leave readiness up and the event stream empty", %{
    http_port: port
  } do
    assert %{status: 200} = HttpClient.request(port, "GET", "/readyz")
    cookie = cookie(port)
    client = events(port, [cookie])
    assert client.body == ": ping\n\n"
    assert %{status: 404} = HttpClient.request(port, "GET", "/tts/clip/clip", headers: [cookie])
    HttpClient.close(client)
  end

  defp events(port, headers) do
    response =
      port
      |> HttpClient.open("GET", "/tts/events", headers: headers)
      |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))

    assert response.status == 200
    assert response.headers["content-type"] == "text/event-stream"
    response
  end

  defp exchange(port, token, headers \\ []) do
    HttpClient.request(port, "POST", "/tts/session",
      headers: [{"content-type", "application/json"} | headers],
      body: Jason.encode!(%{token: token})
    )
  end

  defp cookie(port) do
    %{status: 204, headers: %{"set-cookie" => set_cookie}} = exchange(port, @token)
    {"cookie", set_cookie |> String.split(";", parts: 2) |> hd()}
  end

  defp publish_clip(event_id, user_id \\ @user_id) do
    body =
      Jason.encode!(%{
        clip_key: "#{user_id}/#{event_id}",
        event_id: event_id,
        kind: "follow",
        duration_ms: 1234,
        expires_at: DateTime.utc_now() |> DateTime.add(300) |> DateTime.to_iso8601()
      })

    {:ok, %{body: reply}} = Gnat.request(:gnat, "overlay.tts.#{user_id}", body)
    %{"seq" => seq} = Jason.decode!(reply)
    seq
  end

  defp upload(key, bytes) do
    {:ok, io} = StringIO.open(bytes)
    {:ok, _} = Object.put(:gnat, "TTS_CLIPS", key, io)
    StringIO.close(io)
  end

  defp respond(known_hash) do
    receive do
      {:msg, %{topic: topic, reply_to: reply, body: body}} ->
        payload = response(topic, Jason.decode!(body), known_hash)
        :ok = Gnat.pub(:gnat, reply, Jason.encode!(payload))
        respond(known_hash)
    end
  end

  defp response(@overlay <> "resolve", %{"token_hash" => hash}, hash),
    do: %{user_id: @user_id, generation: @generation}

  defp response(@overlay <> "resolve", _params, _hash), do: %{code: "not_found"}

  defp response(@overlay <> "check", params, _hash),
    do: %{valid: params == %{"user_id" => @user_id, "generation" => @generation}}
end
