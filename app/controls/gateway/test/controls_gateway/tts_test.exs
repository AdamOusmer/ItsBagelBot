# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TtsTest do
  use ControlsGateway.IntegrationCase, async: false
  import ExUnit.CaptureLog

  alias ControlsGateway.Overlays.{Handle, Puller, Tiers}
  alias ControlsGateway.TestSupport.HttpClient
  alias Gnat.Jetstream.API.{Consumer, Object, Stream}

  @moduletag :tmp_dir
  @moduletag jetstream: true

  @user_id 18_446_744_073_709_551_615
  @generation 7
  @overlay "rpc.internal.projector.overlays.tts."
  @token Base.url_encode64(:binary.copy(<<33>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<34>>, 32), padding: false)
  @ttl_ns 300_000_000_000
  @ack_wait_ns 30_000_000_000
  @json [{"content-type", "application/json"}]

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

    if !tags[:no_streams], do: provision(tags[:ack_wait_ns] || @ack_wait_ns)
    for tier <- Tiers.all(), do: start_supervised!({Puller, tier})
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

  test "a published clip reaches an open stream with a handle until it is acked", %{
    http_port: port
  } do
    cookie = cookie(port)
    live = events(port, [cookie])
    publish_clip("first")
    publish_clip("different-account", 42)

    live = await_clips(live, 1)
    assert [%{"event_id" => "first", "handle" => handle} = clip] = clips(live)
    assert clip["clip_key"] == "#{@user_id}/first"
    refute Map.has_key?(clip, "ack")
    assert 2 == stored()

    assert %{status: 204} = post(port, "/tts/progress", %{handles: [handle]}, cookie)
    assert %{status: 204} = post(port, "/tts/ack", %{handle: handle}, cookie)
    eventually(fn -> stored() == 1 end)
    HttpClient.close(live)
  end

  test "handles must be signed for the session user and name an overlay consumer", %{
    http_port: port
  } do
    cookie = cookie(port)
    live = events(port, [cookie])
    publish_clip("guarded")
    live = await_clips(live, 1)
    [%{"handle" => handle}] = clips(live)
    [ack_part, _mac] = String.split(handle, ".")
    ack = Base.url_decode64!(ack_part, padding: false)
    zero_mac = Base.url_encode64(:binary.copy(<<0>>, 32), padding: false)

    foreign_subject = "$JS.ACK.OTHER.durable.1.1.1.1.1"

    for bad <- [
          "nope",
          ack_part <> "." <> zero_mac,
          Handle.sign(42, ack),
          Handle.sign(@user_id, foreign_subject)
        ] do
      assert %{status: 404} = post(port, "/tts/ack", %{handle: bad}, cookie)
      assert %{status: 404} = post(port, "/tts/progress", %{handles: [handle, bad]}, cookie)
    end

    assert %{status: 404} = post(port, "/tts/ack", %{other: handle}, cookie)

    assert %{status: 404} =
             post(port, "/tts/progress", %{handles: List.duplicate(handle, 33)}, cookie)

    assert %{status: 401} =
             HttpClient.request(port, "POST", "/tts/ack",
               headers: @json,
               body: Jason.encode!(%{handle: handle})
             )

    assert 1 == stored()
    HttpClient.close(live)
  end

  test "a clip without an open session waits and is delivered when a session opens", %{
    http_port: port
  } do
    cookie = cookie(port)
    publish_clip("late")
    Process.sleep(300)

    live = port |> events([cookie]) |> await_clips(1)
    assert [%{"event_id" => "late"}] = clips(live)
    HttpClient.close(live)
  end

  test "closing a stream returns its unacked clips to the other open streams", %{http_port: port} do
    cookie = cookie(port)
    first = events(port, [cookie])
    second = events(port, [cookie])
    publish_clip("shared")

    first = await_clips(first, 1)
    second = await_clips(second, 1)
    HttpClient.close(first)

    second = await_clips(second, 2)
    assert [%{"event_id" => "shared"}, %{"event_id" => "shared"}] = clips(second)
    HttpClient.close(second)
  end

  for {name, progress} <- [
        {"progress keeps a clip past the ack wait", true},
        {"a clip without progress is redelivered after the ack wait", false}
      ] do
    @tag ack_wait_ns: 1_000_000_000
    test name, %{http_port: port} do
      cookie = cookie(port)
      live = events(port, [cookie])
      publish_clip("slow")
      live = await_clips(live, 1)
      [%{"handle" => handle}] = clips(live)

      for _ <- 1..6 do
        if unquote(progress), do: post(port, "/tts/progress", %{handles: [handle]}, cookie)
        Process.sleep(400)
      end

      live = HttpClient.drain(live, 200)
      assert length(clips(live)) > 1 == unquote(!progress)
      HttpClient.close(live)
    end
  end

  test "invalid overlay messages are terminated", %{http_port: port} do
    valid = Jason.encode!(%{event_id: "x"})

    for {subject, body} <- [
          {"overlays.premium.tts.#{@user_id}", "not json"},
          {"overlays.premium.tts.abc", valid},
          {"overlays.premium.tts.0", valid}
        ] do
      assert {:ok, _} = Gnat.request(:gnat_jetstream, subject, body)
    end

    eventually(fn -> stored() == 0 end)
    assert %{status: 200} = HttpClient.request(port, "GET", "/readyz")
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

  defp provision(ack_wait_ns) do
    for tier <- Tiers.all() do
      stream = Tiers.stream(tier)

      {:ok, _} =
        Stream.create(:gnat_jetstream, %Stream{
          name: stream,
          subjects: ["overlays.#{tier}.>"],
          retention: :workqueue,
          storage: :memory,
          max_age: @ttl_ns,
          duplicate_window: @ttl_ns
        })

      {:ok, _} =
        Consumer.create(:gnat_jetstream, %Consumer{
          stream_name: stream,
          durable_name: Tiers.durable(tier),
          ack_wait: ack_wait_ns,
          max_deliver: -1,
          max_ack_pending: -1
        })
    end

    {:ok, _} =
      Object.create_bucket(:gnat_jetstream, "OVERLAYS_MEDIA", storage: :file, ttl: @ttl_ns)
  end

  defp publish_clip(event_id, user_id \\ @user_id) do
    body =
      Jason.encode!(%{
        clip_key: "#{user_id}/#{event_id}",
        event_id: event_id,
        kind: "tts",
        duration_ms: 1234,
        expires_at: DateTime.utc_now() |> DateTime.add(300) |> DateTime.to_iso8601()
      })

    {:ok, %{body: reply}} =
      Gnat.request(:gnat_jetstream, "overlays.premium.tts.#{user_id}", body,
        headers: [{"Nats-Msg-Id", event_id}]
      )

    %{"seq" => seq} = Jason.decode!(reply)
    seq
  end

  defp stored(tier \\ :premium) do
    {:ok, %{state: %{messages: messages}}} = Stream.info(:gnat_jetstream, Tiers.stream(tier))
    messages
  end

  defp clips(client) do
    ~r/event: tts\ndata: (.*)\n\n/
    |> Regex.scan(client.body, capture: :all_but_first)
    |> Enum.map(fn [json] -> Jason.decode!(json) end)
  end

  defp await_clips(client, count), do: HttpClient.await(client, &(length(clips(&1)) >= count))

  defp post(port, path, body, cookie) do
    HttpClient.request(port, "POST", path, headers: [cookie | @json], body: Jason.encode!(body))
  end

  defp eventually(fun, attempts \\ 40) do
    cond do
      fun.() ->
        :ok

      attempts == 0 ->
        flunk("condition not met")

      true ->
        Process.sleep(100)
        eventually(fun, attempts - 1)
    end
  end

  defp upload(key, nuid, chunks) do
    Enum.each(chunks, &publish_object_message("$O.OVERLAYS_MEDIA.C.#{nuid}", &1))

    meta = %Object.Meta{
      name: key,
      bucket: "OVERLAYS_MEDIA",
      nuid: nuid,
      size: IO.iodata_length(chunks),
      chunks: length(chunks),
      digest: "SHA-256=" <> Base.url_encode64(:crypto.hash(:sha256, chunks))
    }

    publish_object_message("$O.OVERLAYS_MEDIA.M." <> Base.url_encode64(key), Jason.encode!(meta),
      headers: [{"Nats-Rollup", "sub"}]
    )
  end

  defp publish_object_message(subject, body, opts \\ []) do
    assert {:ok, %{body: ack}} = Gnat.request(:gnat_jetstream, subject, body, opts)
    assert %{"stream" => "OBJ_OVERLAYS_MEDIA", "seq" => seq} = Jason.decode!(ack)
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
