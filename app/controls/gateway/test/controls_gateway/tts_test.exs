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
  @overlay "rpc.internal.projector.overlays.tts."
  @token Base.url_encode64(:binary.copy(<<33>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<34>>, 32), padding: false)
  @ttl_ns 300_000_000_000

  setup tags do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))

    on_exit(fn ->
      Application.put_env(:controls_gateway, :overlay_cookie_key, key)
    end)

    overlay = %{
      hash: :crypto.hash(:sha256, @token) |> Base.encode16(case: :lower),
      check: tags[:overlay_check]
    }

    responder = start_supervised!({Task, fn -> respond(overlay) end})
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @overlay <> "resolve")
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @overlay <> "check")

    if !tags[:no_streams] do
      {:ok, _} =
        Stream.create(:gnat_jetstream, %Stream{
          name: "OVERLAY_TTS",
          subjects: ["overlay.tts.*"],
          storage: :file,
          max_age: @ttl_ns
        })

      {:ok, _} = Object.create_bucket(:gnat_jetstream, "TTS_CLIPS", storage: :file, ttl: @ttl_ns)
    end

    :ok
  end

  test "only the beta routes are mounted and the overlay page sends a restrictive policy", %{
    http_port: port
  } do
    for path <- ["/v1/connect", "/metrics", "/bridge/v1/events"] do
      assert %{status: 404} = HttpClient.request(port, "GET", path)
    end

    assert %{
             status: 200,
             headers: %{
               "cache-control" => "no-store",
               "referrer-policy" => "no-referrer",
               "content-security-policy" => csp
             },
             body: body
           } = HttpClient.request(port, "GET", "/tts")

    assert [["style", style], ["script", script]] =
             Regex.scan(~r{<(style|script)>(.*?)</\1>}s, body, capture: :all_but_first)

    assert csp ==
             "default-src 'none'; script-src '#{csp_hash(script)}'; " <>
               "style-src '#{csp_hash(style)}'; connect-src 'self'; media-src 'self'; " <>
               "base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
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

  test "rejected session exchanges log the reason", %{http_port: port} do
    json = [{"content-type", "application/json"}]

    cases = [
      {json, Jason.encode!(%{token: @unknown}), "unknown_token"},
      {json, "{}", "token_format"},
      {[{"content-type", "text/plain"}], "x", "content_type"}
    ]

    for {headers, body, reason} <- cases do
      log =
        capture_warnings(fn ->
          HttpClient.request(port, "POST", "/tts/session", headers: headers, body: body)
        end)

      assert log =~ "TTS session rejected reason=#{reason}"
    end
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

    assert %{status: 429, headers: %{"retry-after" => "60"}} = exchange(port, @token, [ip])
    assert %{status: 204} = exchange(port, @token, [{"cf-connecting-ip", "192.0.2.2"}])
  end

  test "event status requires a session and returns no stream for a valid session", %{
    http_port: port
  } do
    assert %{status: 401} = HttpClient.request(port, "HEAD", "/tts/events")

    assert %{status: 200, body: "", headers: %{"cache-control" => "no-store"} = headers} =
             HttpClient.request(port, "HEAD", "/tts/events", headers: [cookie(port)])

    refute headers["content-type"] == "text/event-stream"
  end

  @tag overlay_check: %{valid: false}
  test "revoked sessions return 204 for the stream and its status check", %{http_port: port} do
    headers = [cookie(port)]

    for method <- ["GET", "HEAD"] do
      assert %{status: 204, body: ""} =
               HttpClient.request(port, method, "/tts/events", headers: headers)
    end
  end

  @tag overlay_check: %{error: "unavailable"}
  test "event status reports a transient error when revocation cannot be checked", %{
    http_port: port
  } do
    assert %{status: 503} =
             HttpClient.request(port, "HEAD", "/tts/events", headers: [cookie(port)])
  end

  @tag overlay_check: %{error: "unavailable"}
  test "unavailable checks are logged with the user id on status and stream open", %{
    http_port: port
  } do
    headers = [cookie(port)]

    log =
      capture_warnings(fn ->
        for method <- ["HEAD", "GET"] do
          HttpClient.request(port, method, "/tts/events", headers: headers)
        end
      end)

    expected = [
      "TTS check unavailable user_id=#{@user_id}",
      "TTS events status unavailable user_id=#{@user_id}",
      "TTS events unavailable user_id=#{@user_id}"
    ]

    for line <- expected, do: assert(log =~ line)
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

    second = publish_clip("second")
    third = publish_clip("third")

    for {headers, cursor, wanted} <- [
          {[cookie], first, second},
          {[cookie, {"last-event-id", Integer.to_string(second)}], second, third}
        ] do
      resumed = events(port, headers, "/tts/events?after=#{first}")
      data = HttpClient.await(resumed, &String.contains?(&1.body, "id: #{wanted}\ndata: "))
      refute data.body =~ "id: #{cursor}\n"
      HttpClient.close(data)
    end
  end

  test "clip reads stream multiple chunks and isolate accounts", %{http_port: port} do
    chunks = [
      "RIFF" <> :binary.copy(<<1, 2, 3, 4>>, 32_767),
      :binary.copy(<<5, 6, 7, 8>>, 17_233)
    ]

    audio = IO.iodata_to_binary(chunks)
    expected_length = Integer.to_string(byte_size(audio))
    upload("#{@user_id}/clip", "clip-nuid_abc123", chunks)
    upload("42/foreign", "ForeignClip12345", chunks)
    headers = [cookie(port)]

    assert %{
             status: 200,
             body: ^audio,
             headers: %{
               "content-type" => "audio/wav",
               "content-length" => ^expected_length,
               "cache-control" => "no-store"
             }
           } = HttpClient.request(port, "GET", "/tts/clip/clip", headers: headers)

    assert %{status: 404} = HttpClient.request(port, "GET", "/tts/clip/foreign", headers: headers)
  end

  test "revocation closes an open event stream", %{http_port: port} do
    live = events(port, [cookie(port)])

    log =
      capture_with_metadata(:info, fn ->
        :ok = Gnat.pub(:gnat_rpc, "controls.tts.revoked.#{@user_id}", "")
        assert HttpClient.await(live, & &1.done).done
      end)

    assert log =~ "TTS events stream closed user_id=#{@user_id} reason=revoked"
    HttpClient.close(live)
  end

  @tag timeout: 15_000
  test "drain closes every open event stream cleanly and rejects new opens", %{http_port: port} do
    on_exit(&ControlsGateway.Drain.reset!/0)
    cookie = cookie(port)
    streams = [events(port, [cookie]), events(port, [cookie])]

    assert :ok = ControlsGateway.Drain.run()

    for stream <- streams do
      closed = HttpClient.await(stream, & &1.done)
      assert closed.done
      HttpClient.close(closed)
    end

    assert %{status: 503} = HttpClient.request(port, "GET", "/readyz")

    for headers <- [[cookie], []] do
      assert %{status: 503, headers: %{"retry-after" => retry_after}} =
               HttpClient.request(port, "GET", "/tts/events", headers: headers)

      assert String.to_integer(retry_after) > 0
    end
  end

  test "event stream sends a 15-30s retry field when NATS is unavailable", %{http_port: port} do
    cookie = cookie(port)
    stop_supervised!(:gnat_jetstream)

    assert %{
             status: 200,
             headers: %{"content-type" => "text/event-stream"},
             body: body,
             done: true
           } = HttpClient.request(port, "GET", "/tts/events", headers: [cookie])

    assert_retry_range(body, 15_000..30_000)
    refute body =~ "\ndata:"
  end

  @tag no_streams: true
  test "missing JetStream resources leave readiness up and the event stream empty", %{
    http_port: port
  } do
    assert %{status: 200} = HttpClient.request(port, "GET", "/readyz")
    cookie = cookie(port)
    client = events(port, [cookie])
    refute client.body =~ "\ndata:"
    assert %{status: 404} = HttpClient.request(port, "GET", "/tts/clip/clip", headers: [cookie])
    HttpClient.close(client)
  end

  defp capture_warnings(fun), do: capture_with_metadata(:warning, fun)

  defp capture_with_metadata(level, fun) do
    capture_log([level: level, format: "$message $metadata", metadata: [:user_id, :reason]], fun)
  end

  defp csp_hash(content), do: "sha256-" <> Base.encode64(:crypto.hash(:sha256, content))

  defp assert_retry_range(body, range) do
    assert [_, ms_text] = Regex.run(~r/\Aretry: (\d+)\n: ping\n\n/, body)
    assert String.to_integer(ms_text) in range
  end

  defp events(port, headers, path \\ "/tts/events") do
    response =
      port
      |> HttpClient.open("GET", path, headers: headers)
      |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))

    assert %{status: 200, headers: %{"content-type" => "text/event-stream"}, done: false} =
             response

    assert_retry_range(response.body, 3_000..8_000)
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
        kind: "cheer",
        duration_ms: 1234,
        expires_at: DateTime.utc_now() |> DateTime.add(300) |> DateTime.to_iso8601()
      })

    {:ok, %{body: reply}} = Gnat.request(:gnat_jetstream, "overlay.tts.#{user_id}", body)
    %{"seq" => seq} = Jason.decode!(reply)
    seq
  end

  defp upload(key, nuid, chunks) do
    Enum.each(chunks, &publish_object_message("$O.TTS_CLIPS.C.#{nuid}", &1))

    meta = %Object.Meta{
      name: key,
      bucket: "TTS_CLIPS",
      nuid: nuid,
      size: IO.iodata_length(chunks),
      chunks: length(chunks),
      digest: "SHA-256=" <> Base.url_encode64(:crypto.hash(:sha256, chunks))
    }

    publish_object_message("$O.TTS_CLIPS.M." <> Base.url_encode64(key), Jason.encode!(meta),
      headers: [{"Nats-Rollup", "sub"}]
    )
  end

  defp publish_object_message(subject, body, opts \\ []) do
    assert {:ok, %{body: ack}} = Gnat.request(:gnat_jetstream, subject, body, opts)
    assert %{"stream" => "OBJ_TTS_CLIPS", "seq" => seq} = Jason.decode!(ack)
    assert is_integer(seq) and seq > 0
  end

  defp respond(overlay) do
    receive do
      {:msg, %{topic: topic, reply_to: reply, body: body}} ->
        payload = response(topic, Jason.decode!(body), overlay)
        :ok = Gnat.pub(:gnat_rpc, reply, Jason.encode!(payload))
        respond(overlay)
    end
  end

  defp response(@overlay <> "resolve", %{"token_hash" => hash}, %{hash: hash}),
    do: %{user_id: @user_id, generation: @generation}

  defp response(@overlay <> "resolve", _params, _overlay),
    do: %{code: "not_found", error: "overlay token not found"}

  defp response(@overlay <> "check", params, %{check: check}),
    do: check || %{valid: params == %{"user_id" => @user_id, "generation" => @generation}}
end
