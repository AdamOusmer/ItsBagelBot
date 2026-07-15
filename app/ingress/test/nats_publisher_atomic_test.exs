defmodule Ingress.Nats.PublisherAtomicTest do
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

  setup do
    conn = :gnat_bus_pub_atomic_test

    overrides = [
      publish_wire: :atomic,
      publish_batch_size: 3,
      publish_batch_wait_ms: 50,
      publish_batch_inflight: 4
    ]

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

  defp enqueue_cohort do
    for n <- 1..3 do
      assert Publisher.enqueue(
               "twitch.ingress.event.standard",
               ~s({"n":#{n}})
             ) == :ok
    end
  end

  defp collect_cohort do
    for _ <- 1..3 do
      assert_receive {:pub, _topic, _json, opts}, 500
      opts
    end
  end

  # Gnat preps headers into cowlib iodata before the connection call; decode
  # them the same way the single-wire publisher test does.
  defp headers_map(opts) do
    for [key, ": ", value, "\r\n"] <- Keyword.get(opts, :headers, []), into: %{} do
      {String.downcase(key), IO.iodata_to_binary(value)}
    end
  end

  test "a cohort travels as one sequenced atomic batch with a single commit reply", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [first, middle, last] = collect_cohort()

    [first_headers, middle_headers, last_headers] =
      Enum.map([first, middle, last], &headers_map/1)

    batch_id = first_headers["nats-batch-id"]
    assert is_binary(batch_id) and byte_size(batch_id) <= 64
    assert middle_headers["nats-batch-id"] == batch_id
    assert last_headers["nats-batch-id"] == batch_id

    assert first_headers["nats-batch-sequence"] == "1"
    assert middle_headers["nats-batch-sequence"] == "2"
    assert last_headers["nats-batch-sequence"] == "3"

    refute Map.has_key?(first_headers, "nats-batch-commit")
    refute Map.has_key?(middle_headers, "nats-batch-commit")
    assert last_headers["nats-batch-commit"] == "1"

    # Dedup is structurally disabled on both publisher wires.
    refute Map.has_key?(first_headers, "nats-msg-id")
    refute Map.has_key?(middle_headers, "nats-msg-id")
    refute Map.has_key?(last_headers, "nats-msg-id")

    # Only the opening (start errors) and commit (PubAck) messages carry replies.
    start_reply = Keyword.fetch!(first, :reply_to)
    commit_reply = Keyword.fetch!(last, :reply_to)
    assert String.contains?(start_reply, ".bs.")
    assert String.contains?(commit_reply, ".bc.")
    refute Keyword.has_key?(middle, :reply_to)

    # Zero-byte start ack changes nothing; the commit PubAck resolves the batch.
    send(publisher, {:msg, %{topic: start_reply, body: ""}})

    send(
      publisher,
      {:msg,
       %{
         topic: commit_reply,
         body: ~s({"stream":"TWITCH_INGRESS","seq":7,"batch":"#{batch_id}","count":3})
       }}
    )

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 1) == 0
    assert :ets.info(ctx.table, :size) == 0
  end

  test "a definitely rejected commit falls back to dedup-free per-message publishes", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [_first, _middle, last] = collect_cohort()

    commit_reply = Keyword.fetch!(last, :reply_to)

    send(
      publisher,
      {:msg,
       %{
         topic: commit_reply,
         body: ~s({"error":{"code":400,"err_code":10176,"description":"batch incomplete"}})
       }}
    )

    # The negative PubAck proves the cohort was not stored, so a dedup-free
    # per-message re-drive is safe.
    fallback = collect_cohort()

    Enum.each(fallback, fn opts ->
      headers = headers_map(opts)
      refute Map.has_key?(headers, "nats-batch-id")
      refute Map.has_key?(headers, "nats-batch-commit")
      refute Map.has_key?(headers, "nats-msg-id")
      assert Keyword.fetch!(opts, :reply_to) =~ ".s."
    end)

    # Still three pending events awaiting their individual PubAcks.
    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 1) == 3
    assert :ets.info(ctx.table, :size) == 3

    fallback
    |> Enum.map(&Keyword.fetch!(&1, :reply_to))
    |> Enum.with_index(1)
    |> Enum.each(fn {reply, sequence} ->
      send(
        publisher,
        {:msg, %{topic: reply, body: ~s({"stream":"TWITCH_INGRESS","seq":#{sequence}})}}
      )
    end)

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 1) == 0
    assert :ets.info(ctx.table, :size) == 0
  end

  test "a start rejection falls back without waiting for the commit reply", %{
    publisher: publisher,
    ctx: ctx
  } do
    enqueue_cohort()
    [first, _middle, _last] = collect_cohort()

    send(
      publisher,
      {:msg,
       %{
         topic: Keyword.fetch!(first, :reply_to),
         body:
           ~s({"error":{"code":400,"err_code":10174,"description":"batch publish not enabled"}})
       }}
    )

    fallback = collect_cohort()
    assert length(fallback) == 3

    _state = :sys.get_state(publisher)
    assert :atomics.get(ctx.counter, 1) == 3
  end

  test "cohorts past the in-flight batch budget degrade to individual publishes", %{
    ctx: ctx
  } do
    # Saturate the budget directly (index 7 tracks in-flight atomic batches).
    :atomics.put(ctx.counter, 7, 4)

    enqueue_cohort()
    cohort = collect_cohort()

    Enum.each(cohort, fn opts ->
      headers = headers_map(opts)
      refute Map.has_key?(headers, "nats-batch-id")
      assert Keyword.fetch!(opts, :reply_to) =~ ".s."
    end)

    assert :atomics.get(ctx.counter, 9) == 1

    :atomics.put(ctx.counter, 7, 0)
  end
end
