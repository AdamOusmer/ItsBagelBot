# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.OverlayTest do
  use ControlsGateway.IntegrationCase, async: false

  import ControlsGateway.TestSupport.Await

  alias ControlsGateway.Overlays
  alias ControlsGateway.Overlays.Handle
  alias ControlsGateway.TestSupport.{HttpClient, Streams}

  @moduletag :tmp_dir
  @moduletag jetstream: true

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
  @ack "$JS.ACK.OVERLAYS_STANDARD.overlays-standard.1.1.1.1.0"
  @env_keys [:overlay_cookie_key, :overlay_check_ms, :overlay_recheck_ms]
  @overlay %{
    user_id: 7,
    overlay_id: 11,
    kind: "system",
    template: "powered",
    revision: 3,
    generation: 5,
    allowed: true,
    premium: false,
    document: @document
  }

  setup tags do
    previous = Map.new(@env_keys, &{&1, Application.fetch_env(:controls_gateway, &1)})
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))
    Application.put_env(:controls_gateway, :overlay_check_ms, 600_000)
    Application.put_env(:controls_gateway, :overlay_recheck_ms, 100)
    on_exit(fn -> Enum.each(previous, &restore_env/1) end)

    Streams.provision(30_000_000_000)
    Enum.each(Overlays.children(), &start_supervised!/1)

    overlay = Map.merge(@overlay, tags[:overlay] || %{})
    agent = start_supervised!({Agent, fn -> overlay end})
    responder = start_supervised!({Task, fn -> respond(agent) end})
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @resolve)
    %{agent: agent, responder: responder}
  end

  test "the overlay page is the powered page with the same policy", %{http_port: port} do
    assert %{status: 200, headers: %{"content-security-policy" => csp}, body: body} =
             HttpClient.request(port, "GET", "/o")

    assert %{body: ^body, headers: %{"content-security-policy" => ^csp}} =
             HttpClient.request(port, "GET", "/powered")

    assert csp =~
             "img-src 'self' https://cdn.itsbagelbot.com; media-src https://cdn.itsbagelbot.com"

    assert csp =~ "connect-src 'self'"
  end

  test "wrong methods are refused with the allowed method", %{http_port: port} do
    for {method, path, allow} <- [
          {"POST", "/o", "GET"},
          {"GET", "/o/session", "POST"},
          {"GET", "/o/ack", "POST"},
          {"GET", "/o/progress", "POST"},
          {"POST", "/o/doc", "GET"},
          {"POST", "/o/events", "GET, HEAD"}
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
    update_overlay(agent, &Map.put(&1, :generation, 6))
    assert doc_reason(port, headers) == "not_found"

    update_overlay(agent, &Map.put(&1, :gone, true))
    assert doc_reason(port, headers) == "not_found"
  end

  test "events open for a valid session and announce revisions", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :revision, 4))
    check_now()
    data = HttpClient.await(live, &String.contains?(&1.body, "event: revision\ndata: 4\n\n"))
    HttpClient.close(data)
  end

  test "events announce time, key and revision once subscribed", %{http_port: port} do
    live = events(port, [cookie(port)])

    key =
      :crypto.hash(:sha256, "overlay:7:11")
      |> binary_part(0, 9)
      |> Base.url_encode64(padding: false)

    assert %{"now" => now, "key" => ^key, "revision" => 3} = HttpClient.ready(live)
    assert abs(now - System.os_time(:millisecond)) < 5_000
    HttpClient.close(live)
  end

  test "a stream opened without v=2 speaks the legacy protocol", %{http_port: port} do
    live =
      port
      |> HttpClient.open("GET", "/o/events", headers: [cookie(port)])
      |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))

    assert {:ok, %{body: ~s({"accepted":true})}} = offer("powered", %{})
    live = HttpClient.await(live, &String.contains?(&1.body, "event: play\ndata: {}\n\n"))
    refute live.body =~ "event: ready"
    refute live.body =~ "event: alert"
    HttpClient.close(live)
  end

  test "events accept offers for their own template only", %{http_port: port} do
    live = events(port, [cookie(port)])

    assert {:error, :no_responders} = offer("tts", %{"event_id" => "other"})

    assert {:ok, %{body: ~s({"accepted":true})}} = offer("powered", %{"event_id" => "own"})
    HttpClient.close(HttpClient.await_alerts(live, 1))
  end

  test "an alert frame carries its id and the fields the page reads", %{http_port: port} do
    live = events(port, [cookie(port)])
    expires_at = "2026-11-23T12:00:00Z"

    assert {:ok, %{body: ~s({"accepted":true})}} =
             offer("powered", %{"event_id" => "evt-1", "x" => 1}, %{expires_at: expires_at})

    live = HttpClient.await_alerts(live, 1)
    assert live.body =~ "id: evt-1\nevent: alert\ndata: {"

    assert [
             %{
               "event_id" => "evt-1",
               "kind" => "powered",
               "expires_at" => ^expires_at,
               "data" => %{"x" => 1},
               "handle" => handle
             }
           ] = HttpClient.alerts(live)

    assert {:ok, @ack} = Handle.verify(7, handle)
    HttpClient.close(live)
  end

  for {name, from_data, expected} <- [
        {"the alert data", "2026-11-23T10:00:00Z", "2026-11-23T10:00:00Z"},
        {"the offer", nil, "2026-11-23T11:00:00Z"}
      ] do
    test "expires_at comes from #{name}", %{http_port: port} do
      live = events(port, [cookie(port)])
      data = alert_data("exp", unquote(from_data))
      assert {:ok, _reply} = offer("powered", data, %{expires_at: "2026-11-23T11:00:00Z"})
      live = HttpClient.await_alerts(live, 1)
      assert [%{"expires_at" => unquote(expected)}] = HttpClient.alerts(live)
      HttpClient.close(live)
    end
  end

  test "an alert is redelivered when its stream ends before the ack", %{http_port: port} do
    cookie = cookie(port)
    first = events(port, [cookie])
    publish_alert("replayed")
    HttpClient.close(HttpClient.await_alerts(first, 1))

    second = port |> reopen(cookie) |> HttpClient.await_alerts(1)

    assert [%{"event_id" => "replayed", "handle" => handle, "expires_at" => expires_at}] =
             HttpClient.alerts(second)

    assert {:ok, expires, _offset} = DateTime.from_iso8601(expires_at)
    assert DateTime.diff(expires, DateTime.utc_now()) in 240..300
    assert %{status: 204} = post(port, "/o/ack", %{handle: handle}, [cookie])
    eventually(fn -> Streams.stored(:standard) == 0 end)
    HttpClient.close(second)
  end

  test "an alert written to a stream stays queued until the page acks it", %{http_port: port} do
    live = events(port, [cookie(port)])
    publish_alert("unacked")
    live = HttpClient.await_alerts(live, 1)
    assert Streams.stored(:standard) == 1
    HttpClient.close(live)
  end

  test "an offer nobody accepts is requeued with growing delays", _context do
    {:ok, _} = Gnat.sub(:gnat_jetstream, self(), "overlays.session.7.powered")
    publish_alert("ladder")

    stamps =
      for delivered <- 1..3 do
        assert_receive {:msg, %{reply_to: reply, body: body}}, 5_000
        assert %{"delivered" => ^delivered} = Jason.decode!(body)
        :ok = Gnat.pub(:gnat_jetstream, reply, ~s({"accepted":false}))
        System.monotonic_time(:millisecond)
      end

    gaps = stamps |> Enum.chunk_every(2, 1, :discard) |> Enum.map(fn [from, to] -> to - from end)

    for {gap, minimum_ms} <- Enum.zip(gaps, [250, 1_000]) do
      assert gap >= minimum_ms - 20
    end
  end

  test "ack and progress need the session and handles signed for its user", %{http_port: port} do
    cookie = cookie(port)
    good = Handle.sign(7, @ack)
    foreign = Handle.sign(42, @ack)

    cases = [
      {"/o/ack", [], %{handle: good}, 401},
      {"/o/ack", [cookie], %{handle: good}, 204},
      {"/o/ack", [cookie], %{handle: foreign}, 404},
      {"/o/ack", [cookie], %{handle: "nope"}, 404},
      {"/o/ack", [cookie], %{handle: good, reason: "played"}, 204},
      {"/o/ack", [cookie], %{handle: good, reason: "late"}, 404},
      {"/o/progress", [], %{handles: [good]}, 401},
      {"/o/progress", [cookie], %{handles: [good]}, 204},
      {"/o/progress", [cookie], %{handles: [good, foreign]}, 404},
      {"/o/progress", [cookie], %{handles: List.duplicate(good, 33)}, 404}
    ]

    for {path, headers, body, status} <- cases do
      assert %{status: ^status} = post(port, path, body, headers)
    end
  end

  test "ack reasons are counted", %{http_port: port} do
    cookie = cookie(port)
    handle = Handle.sign(7, @ack)

    reasons = [
      {"played", ["Ack/Ack"]},
      {"duplicate", ["Ack/Ack", "Page/DuplicateSkipped"]},
      {"expired", ["Ack/Ack", "Page/Expired"]},
      {"dropped", ["Ack/Ack", "Page/Dropped"]}
    ]

    for {reason, metrics} <- reasons do
      expected = Map.new(metrics, &{&1, counter(&1) + 1})
      assert %{status: 204} = post(port, "/o/ack", %{handle: handle, reason: reason}, [cookie])
      assert Map.new(metrics, &{&1, counter(&1)}) == expected
    end
  end

  test "HEAD mirrors GET for every stream outcome", %{http_port: port, agent: agent} do
    cookie = cookie(port)

    cases = [
      {%{}, [cookie], 200, nil},
      {%{}, [], 401, nil},
      {%{gone: true}, [cookie], 204, nil},
      {%{down: true}, [cookie], 503, "15"},
      {:planes, [cookie], 503, "1"}
    ]

    for {change, headers, status, retry_after} <- cases do
      prepare(change, agent)
      response = HttpClient.request(port, "HEAD", "/o/events", headers: headers)
      assert {response.status, response.headers["retry-after"]} == {status, retry_after}
    end
  end

  test "HEAD during drain is a 503 with retry-after 5", %{http_port: port} do
    on_exit(&ControlsGateway.Drain.reset!/0)
    assert ControlsGateway.Drain.run() == :ok
    response = HttpClient.request(port, "HEAD", "/o/events", headers: [cookie(port)])
    assert {response.status, response.headers["retry-after"]} == {503, "5"}
  end

  for plane <- [:gnat_jetstream, :gnat_rpc] do
    test "opening a stream while #{plane} is down is a 503 with a one second retry", %{
      http_port: port
    } do
      cookie = cookie(port)
      stop_supervised!(unquote(plane))

      assert %{status: 503, headers: %{"retry-after" => "1"}, body: ""} =
               HttpClient.request(port, "GET", "/o/events", headers: [cookie])
    end
  end

  test "a stream ends when its rpc connection drops", %{http_port: port} do
    put_env(:stream_force_close_ms, 300)
    live = events(port, [cookie(port)])
    before = counter("Stream/Closed/rpc_down")
    Process.exit(Platform.Nats.connection_pid(:gnat_rpc), :kill)
    assert HttpClient.await(live, & &1.done).done
    assert counter("Stream/Closed/rpc_down") > before
  end

  test "an unavailable check is retried until the link answers", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :down, true))
    check_now()
    live = HttpClient.drain(live, 300)
    refute live.done
    update_overlay(agent, &(&1 |> Map.delete(:down) |> Map.put(:revision, 4)))
    live = HttpClient.await(live, &String.contains?(&1.body, "event: revision\ndata: 4\n\n"))
    HttpClient.close(live)
  end

  test "a stream survives an unavailable link check", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :down, true))
    check_now()
    refute HttpClient.drain(live, 400).done
    HttpClient.close(live)
  end

  test "a stream ends when its NATS connection drops and the next one gets the offer", %{
    http_port: port
  } do
    cookie = cookie(port)
    live = events(port, [cookie])
    before = counter("Stream/Closed/nats_down")
    Process.exit(Platform.Nats.connection_pid(:gnat_jetstream), :kill)
    assert HttpClient.await(live, & &1.done).done
    assert counter("Stream/Closed/nats_down") > before

    next = reopen(port, cookie)

    assert {:ok, %{body: ~s({"accepted":true})}} =
             offer("powered", %{"event_id" => "after-reconnect"})

    HttpClient.close(HttpClient.await_alerts(next, 1))
  end

  test "events close when the generation changes", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :generation, 6))
    check_now()
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  for {name, id, keyed} <- [
        {"a rotation event for the user", 7, true},
        {"an event for every user", :all, false}
      ] do
    test "#{name} ends a stream whose link check now fails", %{http_port: port, agent: agent} do
      live = events(port, [cookie(port)])
      Agent.update(agent, &Map.put(&1, :gone, true))
      keys = if unquote(keyed), do: [sha(@token)], else: []

      :ok =
        Platform.Invalidations.publish(
          ControlsGateway.Invalidations,
          "overlays",
          unquote(id),
          keys
        )

      closed = HttpClient.await(live, & &1.done)
      assert closed.done
      HttpClient.close(closed)
    end
  end

  test "an unavailable check after a link change keeps retrying until it settles", %{
    http_port: port,
    agent: agent
  } do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :down, true))
    keys = [sha(@token)]
    :ok = Platform.Invalidations.publish(ControlsGateway.Invalidations, "overlays", 7, keys)
    live = HttpClient.drain(live, 600)
    refute live.done

    update_overlay(agent, &(&1 |> Map.delete(:down) |> Map.put(:gone, true)))
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  test "a feed gap sends every open stream to re-check its link", %{
    http_port: port,
    agent: agent,
    responder: responder
  } do
    put_env(:stream_force_close_ms, 60_000)
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.put(&1, :gone, true))
    old = Platform.Nats.connection_pid(:gnat_rpc)
    Process.exit(old, :kill)
    eventually(fn -> Platform.Nats.connection_pid(:gnat_rpc) not in [nil, old] end)
    {:ok, _} = Gnat.sub(:gnat_rpc, responder, @resolve)
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  test "an offer of another kind waits out the timeout when only other kinds are open", %{
    http_port: port
  } do
    live = events(port, [cookie(port)])
    before = counter("Offer/Timeout")

    {:ok, _} =
      Gnat.request(
        :gnat_jetstream,
        "overlays.premium.tts.7",
        Jason.encode!(%{event_id: "foreign"}), headers: [{"Nats-Msg-Id", "foreign"}])

    eventually(fn -> counter("Offer/Timeout") > before end, 60)
    HttpClient.close(live)
  end

  test "an offer reaches a stream subscribed only on the legacy subject" do
    {:ok, _} = Gnat.sub(:gnat_jetstream, self(), "overlays.session.7")
    publish_alert("old-pod")
    assert_receive {:msg, %{reply_to: reply, body: body}}, 5_000
    assert %{"kind" => "powered"} = Jason.decode!(body)
    :ok = Gnat.pub(:gnat_jetstream, reply, ~s({"accepted":true}))
  end

  test "a stream also takes an offer published on the legacy subject", %{http_port: port} do
    live = events(port, [cookie(port)])
    body = offer_body("powered", %{"event_id" => "legacy-in"}, %{})

    assert {:ok, %{body: ~s({"accepted":true})}} =
             Gnat.request(:gnat_jetstream, "overlays.session.7", body)

    HttpClient.close(HttpClient.await_alerts(live, 1))
  end

  @tag overlay: %{kind: "custom", premium: true, document: %{"version" => 2, "kind" => "custom"}}
  test "a custom overlay without a template opens a stream and stays open", %{http_port: port} do
    live = events(port, [cookie(port)])
    assert %{"key" => _key} = HttpClient.ready(live)
    live = HttpClient.drain(live, 500)
    refute live.done
    HttpClient.close(live)
  end

  test "events close when the overlay stops being allowed", %{http_port: port, agent: agent} do
    live = events(port, [cookie(port)])
    update_overlay(agent, &Map.merge(&1, %{kind: "custom", allowed: false, document: nil}))
    check_now()
    closed = HttpClient.await(live, & &1.done)
    assert closed.done
    HttpClient.close(closed)
  end

  @tag overlay: %{kind: "custom", allowed: false, document: nil}
  test "events end with no content when the overlay is not allowed", %{http_port: port} do
    assert %{status: 204, body: ""} =
             HttpClient.request(port, "GET", "/o/events", headers: [cookie(port)])
  end

  @tag overlay: %{premium: true}
  test "the legacy powered config route serves pages already loaded", %{http_port: port} do
    assert %{status: 200, headers: headers, body: body} = legacy_config(port, @token)
    assert headers["cache-control"] == "no-store"
    assert Jason.decode!(body) == %{"config" => @document["config"], "premium" => true}
  end

  @tag overlay: %{kind: "custom", allowed: false, document: nil}
  test "the legacy powered config route refuses unknown, custom and failing links", %{
    http_port: port
  } do
    for {token, status} <- [{@unknown, 404}, {@token, 404}, {@broken, 503}] do
      assert %{status: ^status} = legacy_config(port, token)
    end
  end

  defp update_overlay(agent, fun) do
    Agent.update(agent, fun)
    Platform.Cache.clear(:overlay_resolve)
  end

  defp prepare(:planes, _agent), do: stop_supervised!(:gnat_jetstream)
  defp prepare(change, agent), do: update_overlay(agent, &Map.merge(&1, change))

  defp restore_env({key, {:ok, value}}), do: Application.put_env(:controls_gateway, key, value)
  defp restore_env({key, :error}), do: Application.delete_env(:controls_gateway, key)

  defp alert_data(id, nil), do: %{"event_id" => id}
  defp alert_data(id, expires_at), do: %{"event_id" => id, "expires_at" => expires_at}

  defp offer(kind, data, extra \\ %{}, opts \\ []) do
    Gnat.request(
      :gnat_jetstream,
      "overlays.session.7.#{kind}",
      offer_body(kind, data, extra),
      opts
    )
  end

  defp offer_body(kind, data, extra) do
    %{ack: @ack, kind: kind, data: data, delivered: 1}
    |> Map.merge(extra)
    |> Jason.encode!()
  end

  defp publish_alert(event_id) do
    {:ok, %{body: reply}} =
      Gnat.request(
        :gnat_jetstream,
        "overlays.standard.powered.7",
        Jason.encode!(%{event_id: event_id}),
        headers: [{"Nats-Msg-Id", event_id}]
      )

    %{"seq" => seq} = Jason.decode!(reply)
    seq
  end

  defp post(port, path, body, headers) do
    HttpClient.request(port, "POST", path,
      headers: [{"content-type", "application/json"} | headers],
      body: Jason.encode!(body)
    )
  end

  defp reopen(port, cookie, attempts \\ 40) do
    client =
      port
      |> HttpClient.open("GET", "/o/events?v=2", headers: [cookie])
      |> HttpClient.await(&(String.contains?(&1.body, ": ping\n\n") or &1.done))

    cond do
      not client.done ->
        client

      attempts == 0 ->
        flunk("events did not reopen")

      true ->
        HttpClient.close(client)
        Process.sleep(100)
        reopen(port, cookie, attempts - 1)
    end
  end

  defp check_now do
    pids = Registry.select(ControlsGateway.Connections, [{{:_, :"$1", :_}, [], [:"$1"]}])
    Enum.each(pids, &send(&1, :check))
  end

  defp legacy_config(port, token) do
    HttpClient.request(port, "POST", "/powered/config",
      headers: [{"content-type", "application/json"}],
      body: Jason.encode!(%{token: token})
    )
  end

  defp events(port, headers) do
    response =
      port
      |> HttpClient.open("GET", "/o/events?v=2", headers: headers)
      |> HttpClient.await(&String.contains?(&1.body, ": ping\n\n"))

    assert %{status: 200, headers: %{"content-type" => "text/event-stream"}, done: false} =
             response

    assert [_, retry_ms] = Regex.run(~r/\Aretry: (\d+)\n: ping\n\n/, response.body)
    assert String.to_integer(retry_ms) in 1_000..3_000
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
        "token_hash" => :crypto.hash(:sha256, @token) |> Base.encode16(case: :lower),
        "user_id" => 7
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
      unavailable?(hash, overlay) -> %{error: "projection down", code: "unavailable"}
      known?(hash, overlay) -> reject_nil(overlay)
      true -> %{error: "overlay not found", code: "not_found"}
    end
  end

  defp unavailable?(hash, overlay), do: hash == sha(@broken) or Map.has_key?(overlay, :down)
  defp known?(hash, overlay), do: hash == sha(@token) and live?(overlay)
  defp live?(overlay), do: not Map.has_key?(overlay, :gone)

  defp reject_nil(overlay), do: Map.reject(overlay, fn {_key, value} -> is_nil(value) end)

  defp sha(token), do: :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)
end
