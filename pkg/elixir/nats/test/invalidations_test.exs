# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.InvalidationsTest do
  use ExUnit.Case, async: false

  alias Platform.Invalidations
  alias Platform.Nats
  alias Platform.Nats.TestSupport.NatsServer

  @moduletag :tmp_dir
  @origin_header "Bagel-Invalidation-Origin"

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
    feed = start_feed(:feed_one)
    test_pid = self()
    :ok = Invalidations.watch(feed, "overlays", &send(test_pid, {:event, &1}))
    :ok = Invalidations.watch_gap(feed, fn -> send(test_pid, :gap) end)
    %{feed: feed}
  end

  defp start_feed(name) do
    start_supervised!(
      {Invalidations, name: name, plane: :rpc, prefix: "cache.invalidate", retry_ms: 50}
    )

    name
  end

  defmodule Registrar do
    use GenServer

    def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

    @impl true
    def init(opts) do
      test_pid = Keyword.fetch!(opts, :test_pid)

      :ok =
        Invalidations.watch(
          Keyword.fetch!(opts, :feed),
          "registered",
          &send(test_pid, {:registered, &1})
        )

      {:ok, nil}
    end
  end

  defp await_restart(_name, _old, 0), do: flunk("feed did not restart")

  defp await_restart(name, old, attempts) do
    case Process.whereis(name) do
      pid when is_pid(pid) and pid != old -> :ok
      _ -> Process.sleep(20) && await_restart(name, old, attempts - 1)
    end
  end

  defp publish_raw(subject, body, headers) do
    :ok = Nats.publish(:gnat_rpc, subject, body, headers: headers)
  end

  defp await_subscribed(subject) do
    {:ok, _sid} = Nats.subscribe(:gnat_rpc, self(), subject)
    Process.sleep(100)
  end

  for {name, body, expected} <- [
        {"a numeric id with keys", ~s({"broadcaster_id":"42","keys":["a","b"]}),
         %{scope: "overlays", id: 42, keys: ["a", "b"]}},
        {"the wildcard id", ~s({"broadcaster_id":"*"}), %{scope: "overlays", id: :all, keys: []}},
        {"a null key list", ~s({"broadcaster_id":"7","keys":null}),
         %{scope: "overlays", id: 7, keys: []}}
      ] do
    test "Go-shaped message dispatches #{name}" do
      await_subscribed("cache.invalidate.overlays")
      publish_raw("cache.invalidate.overlays", unquote(body), [{@origin_header, "go-origin"}])

      assert_receive {:event, unquote(Macro.escape(expected))}, 1_000
    end
  end

  for {name, body} <- [
        {"invalid json", "not json"},
        {"a zero id", ~s({"broadcaster_id":"0"})},
        {"a non numeric id", ~s({"broadcaster_id":"abc"})}
      ] do
    test "#{name} is dropped" do
      await_subscribed("cache.invalidate.overlays")
      publish_raw("cache.invalidate.overlays", unquote(body), [])

      refute_receive {:event, _}, 200
    end
  end

  test "an unwatched scope is not delivered" do
    await_subscribed("cache.invalidate.other")
    publish_raw("cache.invalidate.other", ~s({"broadcaster_id":"1"}), [])

    refute_receive {:event, _}, 200
  end

  test "own origin is skipped and a peer feed receives the event", %{feed: feed} do
    peer = start_feed(:feed_two)
    test_pid = self()
    :ok = Invalidations.watch(peer, "overlays", &send(test_pid, {:peer_event, &1}))
    Process.sleep(100)

    :ok = Invalidations.publish(feed, "overlays", 9, ["k"])

    assert_receive {:event, %{id: 9, keys: ["k"]}}, 1_000
    assert_receive {:peer_event, %{id: 9, keys: ["k"]}}, 1_000
    for tag <- [:event, :peer_event], do: refute_receive({^tag, _}, 200)
  end

  test "a named watcher and gap are replaced when the owner registers again", %{feed: feed} do
    test_pid = self()

    for tag <- [:first, :second] do
      :ok = Invalidations.watch(feed, "overlays", &send(test_pid, {tag, &1}), :cache)
      :ok = Invalidations.watch_gap(feed, fn -> send(test_pid, {:gap, tag}) end, :cache)
    end

    :ok = Invalidations.publish(feed, "overlays", 4)
    assert_receive {:second, %{id: 4}}, 1_000
    refute_receive {:first, _}, 200

    Process.exit(Nats.connection_pid(:gnat_rpc), :kill)
    assert_receive {:gap, :second}, 2_000
    refute_receive {:gap, :first}, 200
  end

  test "feed restart keeps cache invalidation working" do
    children = [
      {Invalidations, name: :restart_feed, plane: :rpc, prefix: "cache.invalidate", retry_ms: 50},
      {Registrar, feed: :restart_feed, test_pid: self()}
    ]

    start_supervised!(%{
      id: :restart_supervisor,
      type: :supervisor,
      start: {Supervisor, :start_link, [children, [strategy: :rest_for_one]]}
    })

    old = Process.whereis(:restart_feed)
    Process.exit(old, :kill)
    await_restart(:restart_feed, old, 50)
    Process.sleep(300)
    publish_raw("cache.invalidate.registered", ~s({"broadcaster_id":"6"}), [])

    assert_receive {:registered, %{id: 6}}, 2_000
  end

  test "an entry loaded while the feed was detached is cleared after re-attach", %{feed: feed} do
    table = :ets.new(:detached, [:public, :set])
    :ok = Invalidations.watch_gap(feed, fn -> :ets.delete_all_objects(table) end)

    Process.exit(Nats.connection_pid(:gnat_rpc), :kill)
    :ets.insert(table, {:entry, 1})
    assert_receive :gap, 2_000
    :sys.get_state(feed)

    assert :ets.tab2list(table) == []
  end

  test "a replaced connection fires the gap and the feed subscribes again" do
    old = Nats.connection_pid(:gnat_rpc)
    Process.exit(old, :kill)
    assert_receive :gap, 2_000

    NatsServer.await_connection(:gnat_rpc)
    Process.sleep(300)
    publish_raw("cache.invalidate.overlays", ~s({"broadcaster_id":"5"}), [])

    assert_receive {:event, %{id: 5}}, 2_000
  end
end
