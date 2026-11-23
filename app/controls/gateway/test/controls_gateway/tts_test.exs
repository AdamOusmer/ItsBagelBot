# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.TtsTest do
  use ControlsGateway.IntegrationCase, async: false
  import ControlsGateway.TestSupport.Await
  import ExUnit.CaptureLog

  alias ControlsGateway.Drain
  alias ControlsGateway.Overlays
  alias ControlsGateway.Overlays.{Handle, Tiers}
  alias ControlsGateway.TestSupport.{HttpClient, Peer, Streams}
  alias Gnat.Jetstream.API.{Consumer, Object}
  alias Platform.Nats.TestSupport.NatsServer

  @moduletag :tmp_dir
  @moduletag jetstream: true

  @user_id 18_446_744_073_709_551_615
  @generation 7
  @overlay "rpc.internal.projector.overlays.tts."
  @token Base.url_encode64(:binary.copy(<<33>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<34>>, 32), padding: false)
  @ack_wait_ns 30_000_000_000
  @json [{"content-type", "application/json"}]

  setup tags do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))

    on_exit(fn ->
      Application.put_env(:controls_gateway, :overlay_cookie_key, key)
    end)

    check = start_supervised!({Agent, fn -> tags[:overlay_check] end})

    overlay = %{
      hash: :crypto.hash(:sha256, @token) |> Base.encode16(case: :lower),
      check: check
    }

    responder = start_supervised!({Task, fn -> respond(overlay) end})
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @overlay <> "resolve")
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @overlay <> "check")

    if !tags[:no_streams], do: Streams.provision(tags[:ack_wait_ns] || @ack_wait_ns)
    Enum.each(Overlays.children(), &start_supervised!/1)
    %{check: check}
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

  test "oversized session and ack bodies are refused", %{http_port: port} do
    padding = String.duplicate("a", 300)

    log =
      capture_warnings(fn ->
        assert %{status: 404} =
                 HttpClient.request(port, "POST", "/tts/session",
                   headers: @json,
                   body: Jason.encode!(%{token: @token, padding: padding})
                 )
      end)

    assert log =~ "TTS session rejected reason=body_size"

    assert %{status: 404} =
             post(port, "/tts/ack", %{handle: String.duplicate("a", 9_000)}, cookie(port))
  end

  test "an offer with no open stream is requeued at once instead of timing out", %{
    http_port: port
  } do
    before = no_responders()
    publish_clip("nobody")
    eventually(fn -> no_responders() > before end, 10)
    assert %{status: 200} = HttpClient.request(port, "GET", "/healthz")
  end

  for plane <- [:rpc_port, :jetstream_port] do
    test "readiness is 503 while the #{plane} server is stopped", %{http_port: port} = context do
      await_ready(port)
      NatsServer.stop(Map.fetch!(context, unquote(plane)))
      eventually(fn -> readyz(port) == 503 end)
    end
  end

  test "readiness follows the rpc plane and the pullers", %{http_port: port, rpc_port: rpc_port} do
    await_ready(port)
    NatsServer.stop(rpc_port)
    eventually(fn -> readyz(port) == 503 end)
    NatsServer.restart(rpc_port)
    await_ready(port)

    Overlays.pause_pullers()
    assert readyz(port) == 503
  end

  test "drain waits for an in-flight offer before closing", %{http_port: port} do
    on_exit(&ControlsGateway.Drain.reset!/0)
    await_ready(port)
    {:ok, _} = Gnat.sub(:gnat_jetstream, self(), "overlays.session.#{@user_id}.tts")
    publish_clip("inflight")
    assert_receive {:msg, %{reply_to: reply}}, 2_000

    drain = Task.async(&ControlsGateway.Drain.run/0)
    assert Task.yield(drain, 500) == nil

    :ok = Gnat.pub(:gnat_jetstream, reply, ~s({"accepted":true}))
    assert Task.await(drain, 5_000) == :ok
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

  test "exchanges are not limited by client address", %{http_port: port} do
    ip = {"cf-connecting-ip", "192.0.2.1"}

    for _ <- 1..20 do
      assert %{status: 404} = exchange(port, @unknown, [ip])
    end

    assert %{status: 204} = exchange(port, @token, [ip])
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

  test "each clip reaches exactly one stream and returns to the other when it closes", %{
    http_port: port
  } do
    cookie = cookie(port)
    streams = [events(port, [cookie]), events(port, [cookie])]
    publish_clip("shared")

    streams = Enum.map(streams, &HttpClient.drain(&1, 700))
    assert {[holder], [idle]} = Enum.split_with(streams, &(length(clips(&1)) == 1))
    assert [%{"event_id" => "shared"}] = clips(holder)
    HttpClient.close(holder)

    idle = await_clips(idle, 1)
    assert [%{"event_id" => "shared"}] = clips(idle)
    HttpClient.close(idle)
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
    await_ready(port)
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
    put_env(:stream_force_close_ms, 300)
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

  for plane <- [:gnat_jetstream, :gnat_rpc] do
    test "opening a stream while #{plane} is down is a 503 with a one second retry", %{
      http_port: port
    } do
      cookie = cookie(port)
      stop_supervised!(unquote(plane))

      assert %{status: 503, headers: %{"retry-after" => "1"}, body: ""} =
               HttpClient.request(port, "GET", "/tts/events", headers: [cookie])
    end
  end

  @tag overlay_check: %{error: "unavailable"}
  test "a stream opened while the projector is unavailable gets a 15-30s retry field", %{
    http_port: port
  } do
    assert %{
             status: 200,
             headers: %{"content-type" => "text/event-stream"},
             body: body,
             done: true
           } = HttpClient.request(port, "GET", "/tts/events", headers: [cookie(port)])

    assert_retry_range(body, 15_000..30_000)
    refute body =~ "event: ready"
  end

  for {name, plane, reason} <- [
        {"rpc", :gnat_rpc, "rpc_down"},
        {"jetstream", :gnat_jetstream, "nats_down"}
      ] do
    test "a stream closes when the #{name} connection drops", %{http_port: port} do
      put_env(:stream_force_close_ms, 300)
      live = events(port, [cookie(port)])
      before = counter("Stream/Closed/#{unquote(reason)}")
      Process.exit(Platform.Nats.connection_pid(unquote(plane)), :kill)
      assert HttpClient.await(live, & &1.done).done
      assert counter("Stream/Closed/#{unquote(reason)}") > before
    end
  end

  test "ready carries an opaque overlay key and no revision", %{http_port: port} do
    live = events(port, [cookie(port)])

    key =
      :crypto.hash(:sha256, "tts:#{@user_id}:tts")
      |> binary_part(0, 9)
      |> Base.url_encode64(padding: false)

    assert %{"key" => ^key} = ready = HttpClient.ready(live)
    refute Map.has_key?(ready, "revision")
    HttpClient.close(live)
  end

  test "an alert frame carries its id and the fields the page reads", %{http_port: port} do
    live = events(port, [cookie(port)])
    publish_clip("framed")
    live = await_clips(live, 1)
    assert live.body =~ "id: framed\nevent: alert\ndata: {"

    assert [%{"kind" => "tts", "event_id" => "framed", "expires_at" => expires_at} = alert] =
             HttpClient.alerts(live)

    assert {:ok, _ack} = Handle.verify(@user_id, alert["handle"])
    assert is_binary(expires_at)
    clip_key = "#{@user_id}/framed"
    assert %{"clip_key" => ^clip_key, "duration_ms" => 1234} = alert["data"]
    HttpClient.close(live)
  end

  for source <- [:drain, :gnat_rpc, :gnat_jetstream] do
    test "a stream is told to reconnect and then closed when #{source}", %{http_port: port} do
      on_exit(&Drain.reset!/0)
      put_env(:stream_force_close_ms, 300)
      put_env(:stream_down_grace_ms, 300)
      live = events(port, [cookie(port)])
      task = trigger(unquote(source))
      live = HttpClient.await(live, & &1.done)

      assert [_, after_ms] =
               Regex.run(~r/event: reconnect\ndata: \{"after_ms": (\d+)\}\n\n/, live.body)

      assert String.to_integer(after_ms) in 0..1_500
      finish(task)
    end
  end

  @tag timeout: 60_000
  test "a second gateway takes the next offer and the draining stream closes after the handoff",
       %{http_port: port} = context do
    on_exit(&Drain.reset!/0)
    put_env(:stream_force_close_ms, 20_000)
    cookie = cookie(port)
    old = events(port, [cookie])
    peer_port = Peer.start_gateway(context)
    drain = Task.async(&Drain.run/0)
    old = HttpClient.await(old, &String.contains?(&1.body, "event: reconnect"))
    await_ready(peer_port)
    replacement = events(peer_port, [cookie])
    publish_clip("handoff")

    [old, replacement] = await_clip_total([old, replacement], "handoff")
    refute old.done

    HttpClient.close(old)
    assert Task.await(drain, 10_000) == :ok
    publish_clip("after")
    replacement = HttpClient.await(replacement, &("after" in event_ids(&1)))
    refute replacement.done
    HttpClient.close(replacement)
  end

  test "a stream closed by the client after its reconnect frame leaves its clips in flight", %{
    http_port: port
  } do
    live = events(port, [cookie(port)])
    publish_clip("playing")
    live = await_clips(live, 1)
    before = counter("Ack/ProgressOnHandoff")
    on_exit(&Drain.reset!/0)
    put_env(:stream_force_close_ms, 20_000)
    drain = Task.async(&Drain.run/0)
    live = HttpClient.await(live, &String.contains?(&1.body, "event: reconnect"))
    HttpClient.close(live)
    eventually(fn -> counter("Ack/ProgressOnHandoff") > before end, 100)
    assert Task.await(drain, 10_000) == :ok
    assert stored() == 1

    assert {:ok, %{num_ack_pending: 1}} =
             Consumer.info(:gnat_jetstream, Tiers.stream(:premium), Tiers.durable(:premium))
  end

  for id <- [@user_id, :all] do
    test "a rotation event for #{inspect(id)} ends a stream whose check now fails", %{
      http_port: port,
      check: check
    } do
      live = events(port, [cookie(port)])
      Agent.update(check, fn _ -> %{valid: false} end)
      :ok = Platform.Invalidations.publish(ControlsGateway.Invalidations, "overlays", unquote(id))
      closed = HttpClient.await(live, & &1.done)
      assert closed.done
      HttpClient.close(closed)
    end
  end

  test "drain waits for an in-flight clip download and reports its time", %{http_port: _port} do
    on_exit(&Drain.reset!/0)
    Process.register(self(), :metrics_probe)

    download =
      Task.async(fn ->
        Drain.track_connection(:download)

        receive do
          :finish -> :ok
        end
      end)

    eventually(fn -> Registry.lookup(ControlsGateway.Connections, download.pid) != [] end)
    drain = Task.async(&Drain.run/0)
    assert Task.yield(drain, 600) == nil
    send(download.pid, :finish)
    assert Task.await(drain, 5_000) == :ok
    assert_receive {:metric, "Custom/ControlsGateway/Drain/DurationMs", elapsed_ms}, 1_000
    assert elapsed_ms >= 600
  end

  test "a real clip download registers with the drain until it finishes", %{http_port: port} do
    on_exit(&Drain.reset!/0)
    chunks = for n <- 1..48, do: :binary.copy(<<n>>, 524_288)
    upload("#{@user_id}/big", "bigclip_nuid_123", chunks)

    client = HttpClient.open(port, "GET", "/tts/clip/big", headers: [cookie(port)])
    assert client.status == 200
    eventually(fn -> downloads() == 1 end)

    drain = Task.async(&Drain.run/0)
    assert Task.yield(drain, 700) == nil
    HttpClient.close(client)
    assert Task.await(drain, 10_000) == :ok
  end

  test "streams still open at the drain deadline are counted", %{http_port: port} do
    on_exit(&Drain.reset!/0)
    put_env(:drain_deadline_ms, 300)
    put_env(:stream_force_close_ms, 60_000)
    live = events(port, [cookie(port)])
    Process.register(self(), :metrics_probe)
    assert Drain.run() == :ok
    assert_receive {:metric, "Custom/ControlsGateway/Drain/StreamsLeft", 1}, 1_000
    HttpClient.close(live)
  end

  @tag no_streams: true
  test "missing JetStream resources take readiness down and leave the event stream empty", %{
    http_port: port
  } do
    assert 503 == readyz(port)
    cookie = cookie(port)
    client = events(port, [cookie])
    refute client.body =~ "event: alert"
    assert %{status: 404} = HttpClient.request(port, "GET", "/tts/clip/clip", headers: [cookie])
    HttpClient.close(client)
  end

  defp trigger(:drain), do: Task.async(&Drain.run/0)

  defp trigger(plane) do
    Process.exit(Platform.Nats.connection_pid(plane), :kill)
    nil
  end

  defp finish(nil), do: :ok
  defp finish(task), do: assert(Task.await(task, 10_000) == :ok)

  defp downloads do
    ControlsGateway.Connections
    |> Registry.select([{{:_, :_, :download}, [], [true]}])
    |> length()
  end

  defp readyz(port), do: HttpClient.request(port, "GET", "/readyz").status

  defp await_ready(port), do: eventually(fn -> readyz(port) == 200 end)

  defp no_responders, do: counter("Offer/NoResponders")

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

    assert_retry_range(response.body, 1_000..3_000)
    assert %{"now" => _now, "key" => _key} = HttpClient.ready(response)
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

  defp stored(tier \\ :premium), do: Streams.stored(tier)

  defp clips(client) do
    client
    |> HttpClient.alerts()
    |> Enum.map(fn alert -> Map.put(alert["data"], "handle", alert["handle"]) end)
  end

  defp await_clips(client, count), do: HttpClient.await_alerts(client, count)

  defp await_clip_total(clients, event_id, attempts \\ 60) do
    clients = Enum.map(clients, &HttpClient.drain(&1, 100))
    total = clients |> Enum.map(&clip_count(&1, event_id)) |> Enum.sum()

    cond do
      total > 0 -> settle_clip_total(clients, event_id)
      attempts == 0 -> flunk("clip #{event_id} was never delivered")
      true -> await_clip_total(clients, event_id, attempts - 1)
    end
  end

  defp settle_clip_total(clients, event_id) do
    clients = Enum.map(clients, &HttpClient.drain(&1, 500))
    assert clients |> Enum.map(&clip_count(&1, event_id)) |> Enum.sum() == 1
    clients
  end

  defp clip_count(client, event_id), do: client |> event_ids() |> Enum.count(&(&1 == event_id))

  defp event_ids(client), do: client |> clips() |> Enum.map(& &1["event_id"])

  defp post(port, path, body, cookie) do
    HttpClient.request(port, "POST", path, headers: [cookie | @json], body: Jason.encode!(body))
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
    do:
      Agent.get(check, & &1) ||
        %{valid: params == %{"user_id" => @user_id, "generation" => @generation}}
end
