# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.ProjectorCacheTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.TestSupport.HttpClient

  @tts "rpc.internal.projector.overlays.tts."
  @overlay_resolve "rpc.internal.projector.overlays.resolve"
  @user_id 9
  @generation 2
  @caches [:overlay_resolve, :tts_resolve, :tts_check]
  @sessions ["/tts/session", "/o/session"]
  @token Base.url_encode64(:binary.copy(<<81>>, 32), padding: false)
  @unknown Base.url_encode64(:binary.copy(<<82>>, 32), padding: false)
  @overlay %{
    user_id: @user_id,
    overlay_id: 13,
    kind: "system",
    template: "powered",
    revision: 1,
    generation: @generation,
    allowed: true,
    premium: false,
    document: %{"template" => "powered", "config" => %{}}
  }

  setup do
    key = Application.get_env(:controls_gateway, :overlay_cookie_key)
    Application.put_env(:controls_gateway, :overlay_cookie_key, :crypto.strong_rand_bytes(32))
    on_exit(fn -> Application.put_env(:controls_gateway, :overlay_cookie_key, key) end)

    projector = start_supervised!({Agent, fn -> %{mode: :up, valid: true, calls: %{}} end})
    responder = start_supervised!({Task, fn -> respond(projector) end})

    for subject <- [@overlay_resolve, @tts <> "resolve", @tts <> "check"],
        do: {:ok, _} = Gnat.sub(:gnat_rpc, responder, subject)

    %{projector: projector}
  end

  test "a dead link costs one resolve request per cache", %{http_port: port, projector: projector} do
    for {path, subject} <- [{"/tts/session", @tts <> "resolve"}, {"/o/session", @overlay_resolve}] do
      for _ <- 1..5, do: assert(%{status: 404} = exchange(port, path, @unknown))
      assert calls(projector, subject) == 1
    end
  end

  test "an unavailable reply is not stored", %{http_port: port, projector: projector} do
    for path <- @sessions do
      Agent.update(projector, &%{&1 | mode: :down})
      assert %{status: 503} = exchange(port, path, @token)
      Agent.update(projector, &%{&1 | mode: :up})
      assert %{status: 204} = exchange(port, path, @token)
    end
  end

  test "the status check is served from the cache until the user is invalidated", %{
    http_port: port,
    projector: projector
  } do
    cookie = tts_cookie(port)
    assert %{status: 200} = head(port, cookie)
    assert %{status: 200} = head(port, cookie)
    assert calls(projector, @tts <> "check") == 1

    Agent.update(projector, &%{&1 | valid: false})

    eventually(fn ->
      publish(@user_id |> Integer.to_string() |> invalidation([]))
      head(port, cookie).status == 204
    end)

    assert calls(projector, @tts <> "check") == 2
  end

  test "invalidations still reach the caches after the feed restarts", %{
    http_port: port,
    projector: projector
  } do
    cookie = tts_cookie(port)
    assert %{status: 200} = head(port, cookie)
    feed = Process.whereis(ControlsGateway.Invalidations)
    Process.exit(feed, :kill)

    eventually(fn ->
      restarted = Process.whereis(ControlsGateway.Invalidations)
      is_pid(restarted) and restarted != feed
    end)

    assert %{status: 200} = head(port, cookie)
    Agent.update(projector, &%{&1 | valid: false})
    before = calls(projector, @tts <> "check")

    eventually(fn ->
      publish(@user_id |> Integer.to_string() |> invalidation([]))
      head(port, cookie).status == 204
    end)

    assert calls(projector, @tts <> "check") == before + 1
  end

  test "a TTS invalidation naming only the new hash drops the old hash too", %{
    http_port: port,
    projector: projector
  } do
    subject = @tts <> "resolve"
    assert %{status: 204} = exchange(port, "/tts/session", @token)
    before = calls(projector, subject)

    eventually(fn ->
      publish(invalidation(Integer.to_string(@user_id), [sha(@unknown)]))
      exchange(port, "/tts/session", @token)
      calls(projector, subject) > before
    end)
  end

  test "a wildcard invalidation clears every cache", %{http_port: port} do
    warm(port)

    eventually(fn ->
      publish(invalidation("*", []))
      Enum.all?(@caches, &(Platform.Cache.size(&1) == 0))
    end)
  end

  test "losing the NATS connection clears every cache", %{http_port: port} do
    warm(port)
    Process.exit(Platform.Nats.connection_pid(:gnat_rpc), :kill)
    eventually(fn -> Enum.all?(@caches, &(Platform.Cache.size(&1) == 0)) end)
  end

  defp warm(port) do
    assert %{status: 204} = exchange(port, "/o/session", @token)
    assert %{status: 200} = port |> tts_cookie() |> then(&head(port, &1))
    assert Enum.all?(@caches, &(Platform.Cache.size(&1) == 1))
  end

  defp head(port, cookie), do: HttpClient.request(port, "HEAD", "/tts/events", headers: [cookie])

  defp exchange(port, path, token) do
    HttpClient.request(port, "POST", path,
      headers: [{"content-type", "application/json"}],
      body: Jason.encode!(%{token: token})
    )
  end

  defp tts_cookie(port) do
    %{status: 204, headers: %{"set-cookie" => set_cookie}} =
      exchange(port, "/tts/session", @token)

    {"cookie", set_cookie |> String.split(";", parts: 2) |> hd()}
  end

  defp invalidation(id, keys), do: Jason.encode!(%{broadcaster_id: id, keys: keys})

  defp publish(body), do: :ok = Gnat.pub(:gnat_rpc, "cache.invalidate.overlays", body)

  defp calls(projector, subject), do: Agent.get(projector, &Map.get(&1.calls, subject, 0))

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
      {:msg, %{topic: topic, reply_to: reply, body: body}} ->
        state = Agent.get_and_update(projector, &{&1, count_call(&1, topic)})
        payload = response(topic, Jason.decode!(body), state)
        :ok = Gnat.pub(:gnat_rpc, reply, Jason.encode!(payload))
        respond(projector)
    end
  end

  defp count_call(state, topic),
    do: update_in(state.calls, &Map.update(&1, topic, 1, fn n -> n + 1 end))

  defp response(_topic, _params, %{mode: :down}),
    do: %{error: "projection down", code: "unavailable"}

  defp response(@tts <> "check", _params, %{valid: valid}), do: %{valid: valid}
  defp response(topic, %{"token_hash" => hash}, _state), do: known(topic, hash == sha(@token))

  defp known(@overlay_resolve, true), do: @overlay
  defp known(@tts <> "resolve", true), do: %{user_id: @user_id, generation: @generation}
  defp known(_topic, false), do: %{error: "overlay not found", code: "not_found"}

  defp sha(token), do: :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)
end
