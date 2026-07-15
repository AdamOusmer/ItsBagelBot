defmodule Ingress.Nats.PublisherAtMostOnceTest do
  # async: false — the publisher uses a named process, a named ETS table and a
  # global persistent_term context, so it cannot share the VM with a parallel
  # instance of itself.
  use ExUnit.Case, async: false

  alias Ingress.Nats.Publisher

  defmodule FakeGnat do
    use GenServer

    def start_link(opts),
      do: GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))

    def init(opts), do: {:ok, %{test: Keyword.fetch!(opts, :test), sid: 0}}

    def handle_call({:sub, _receiver, _topic, _opts}, _from, state) do
      {:reply, {:ok, state.sid + 1}, %{state | sid: state.sid + 1}}
    end

    def handle_call({:pub, topic, message, opts}, _from, state) do
      send(state.test, {:pub, topic, message, opts})
      {:reply, :ok, state}
    end
  end

  setup context do
    conn = :gnat_bus_pub_at_most_once_test

    overrides =
      [publish_batch_size: 8, publish_batch_wait_ms: 10] ++
        Map.get(context, :overrides, [])

    previous = Enum.map(overrides, fn {key, _} -> {key, Application.get_env(:ingress, key)} end)
    Enum.each(overrides, fn {key, value} -> Application.put_env(:ingress, key, value) end)

    start_supervised!({FakeGnat, [name: conn, test: self()]})
    start_supervised!({Publisher, [index: 0, conn: conn]})
    :persistent_term.put({Publisher, :n}, 1)

    on_exit(fn ->
      :persistent_term.erase({Publisher, :n})

      Enum.each(previous, fn
        {key, nil} -> Application.delete_env(:ingress, key)
        {key, value} -> Application.put_env(:ingress, key, value)
      end)
    end)

    %{publisher: Publisher.process_name(0), ctx: :persistent_term.get({Publisher, :ctx, 0})}
  end

  defp headers_map(opts) do
    for [key, ": ", value, "\r\n"] <- Keyword.get(opts, :headers, []), into: %{} do
      {String.downcase(key), IO.iodata_to_binary(value)}
    end
  end

  # Rewrites every pending row's timestamp into the past so the next sweep
  # treats it as expired, without waiting out the real ack timeout. Ages
  # relative to the current monotonic clock — its absolute value is an
  # arbitrary (typically negative) offset.
  defp age_pending_rows(ctx) do
    expired = System.monotonic_time(:millisecond) - 10_000_000

    for row <- :ets.tab2list(ctx.table) do
      aged =
        case row do
          {id, :single, subject, json, attempts, _ts} ->
            {id, :single, subject, json, attempts, expired}

          {id, :batch, entries, _ts} ->
            {id, :batch, entries, expired}
        end

      :ets.insert(ctx.table, aged)
    end
  end

  test "the publisher never emits or stores a Nats-Msg-Id", %{ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500

    refute Map.has_key?(headers_map(opts), "nats-msg-id")

    # The pending-row shape contains no dormant dedup-id slot.
    assert [{_id, :single, _subject, _json, 1, _ts}] = :ets.tab2list(ctx.table)
  end

  test "an ambiguous ack timeout drops instead of retrying", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, _opts}, 500

    age_pending_rows(ctx)
    send(publisher, :sweep)
    _state = :sys.get_state(publisher)

    # Dropped, not re-published.
    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    # Counter 5 is failed, counter 4 is retried.
    assert :atomics.get(ctx.counter, 5) == 1
    assert :atomics.get(ctx.counter, 4) == 0
  end

  test "a definite error PubAck still retries without dedup", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500
    reply = Keyword.fetch!(opts, :reply_to)

    # An error PubAck means the broker did not store the event; the retry
    # cannot double-store even without a dedup id.
    send(
      publisher,
      {:msg, %{topic: reply, body: ~s({"error":{"code":503,"description":"no responders"}})}}
    )

    assert_receive {:pub, _topic, _json, retry_opts}, 500
    refute Map.has_key?(headers_map(retry_opts), "nats-msg-id")

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 4) == 1
    assert :atomics.get(ctx.counter, 1) == 1
  end

  test "a malformed single PubAck drops instead of retrying", %{publisher: publisher, ctx: ctx} do
    assert Publisher.enqueue("twitch.ingress.event.standard", "{}") == :ok
    assert_receive {:pub, _topic, _json, opts}, 500

    send(publisher, {:msg, %{topic: Keyword.fetch!(opts, :reply_to), body: "truncated"}})
    _state = :sys.get_state(publisher)

    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 1
    assert :atomics.get(ctx.counter, 4) == 0
  end

  @tag overrides: [publish_wire: :atomic, publish_batch_size: 3]
  test "an expired unprotected atomic batch is dropped whole", %{
    publisher: publisher,
    ctx: ctx
  } do
    for n <- 1..3 do
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":#{n}})) == :ok
    end

    for _ <- 1..3, do: assert_receive({:pub, _, _, _}, 500)
    assert [{_id, :batch, _entries, _ts}] = :ets.tab2list(ctx.table)

    age_pending_rows(ctx)
    send(publisher, :sweep)
    _state = :sys.get_state(publisher)

    # No per-message re-drive: the commit may have landed, so an unprotected
    # cohort must not be re-published.
    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 3
    assert :atomics.get(ctx.counter, 7) == 0
  end

  @tag overrides: [publish_wire: :atomic, publish_batch_size: 3]
  test "a malformed atomic commit PubAck drops the cohort without fallback", %{
    publisher: publisher,
    ctx: ctx
  } do
    for n <- 1..3 do
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":#{n}})) == :ok
    end

    publishes = for _ <- 1..3, do: assert_receive({:pub, _, _, opts}, 500) && opts

    commit =
      Enum.find_value(publishes, fn opts ->
        case Keyword.get(opts, :reply_to) do
          nil -> nil
          reply -> if String.contains?(reply, ".bc."), do: reply
        end
      end)

    assert is_binary(commit)
    send(publisher, {:msg, %{topic: commit, body: "truncated"}})
    _state = :sys.get_state(publisher)

    refute_receive {:pub, _, _, _}, 100
    assert :ets.info(ctx.table, :size) == 0
    assert :atomics.get(ctx.counter, 1) == 0
    assert :atomics.get(ctx.counter, 5) == 3
    assert :atomics.get(ctx.counter, 7) == 0
  end
end
