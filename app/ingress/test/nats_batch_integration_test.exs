defmodule Ingress.NatsCohortIntegrationTest do
  use ExUnit.Case, async: false

  alias Ingress.Nats.Publisher

  @moduletag :integration

  @stream "ELIXIR_BATCH_TEST"

  test "Gnat-managed connection receives individual PubAcks from NATS" do
    with_integration_publisher([], fn conn, ctx ->
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":1}), "elixir-1") == :ok
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":2}), "elixir-2") == :ok

      assert eventually(fn -> :atomics.get(ctx.counter, 1) == 0 end)
      assert stream_messages(conn) == 2
    end)
  end

  test "atomic wire lands whole cohorts with one commit PubAck" do
    with_integration_publisher([publish_wire: :atomic, publish_batch_size: 3], fn conn, ctx ->
      for n <- 1..3 do
        assert Publisher.enqueue(
                 "twitch.ingress.event.standard",
                 ~s({"n":#{n}}),
                 "elixir-atomic-#{n}"
               ) == :ok
      end

      assert eventually(fn -> :atomics.get(ctx.counter, 1) == 0 end)
      assert stream_messages(conn) == 3
      assert :ets.info(ctx.table, :size) == 0
    end)
  end

  test "atomic wire cannot double-store a replayed cohort" do
    with_integration_publisher([publish_wire: :atomic, publish_batch_size: 3], fn conn, ctx ->
      replay = fn ->
        for n <- 1..3 do
          assert Publisher.enqueue(
                   "twitch.ingress.event.standard",
                   ~s({"n":#{n}}),
                   "elixir-replay-#{n}"
                 ) == :ok
        end

        assert eventually(fn -> :atomics.get(ctx.counter, 1) == 0 end)
      end

      # First pass stores the cohort; the second carries ids already inside the
      # dedup window — whether the broker folds the batch or rejects it into
      # the per-message fallback, the stream must not grow.
      replay.()
      assert stream_messages(conn) == 3
      replay.()
      assert stream_messages(conn) == 3
    end)
  end

  defp with_integration_publisher(overrides, run) do
    port = System.get_env("NATS_INTEGRATION_PORT")

    if is_nil(port) do
      :ok
    else
      port = String.to_integer(port)
      conn = :gnat_batch_integration

      overrides =
        Keyword.merge([publish_batch_size: 2, publish_batch_wait_ms: 100], overrides)

      previous =
        Enum.map(overrides, fn {key, _} -> {key, Application.get_env(:ingress, key)} end)

      Enum.each(overrides, fn {key, value} -> Application.put_env(:ingress, key, value) end)

      {:ok, gnat} = Gnat.start_link(%{host: ~c"127.0.0.1", port: port}, name: conn)
      ensure_stream(conn)
      purge_stream(conn)
      start_supervised!({Publisher, [index: 0, conn: conn]})
      :persistent_term.put({Publisher, :n}, 1)

      on_exit(fn ->
        # The Gnat connection is linked to the test process, so it is already
        # gone when on_exit runs — only VM-global bookkeeping can be restored
        # here. Broker-side cleanup happens in the `after` below, while the
        # connection is still alive.
        if Process.alive?(gnat), do: GenServer.stop(gnat)
        :persistent_term.erase({Publisher, :n})

        Enum.each(previous, fn
          {key, nil} -> Application.delete_env(:ingress, key)
          {key, value} -> Application.put_env(:ingress, key, value)
        end)
      end)

      try do
        run.(conn, :persistent_term.get({Publisher, :ctx, 0}))
      after
        # Remove the isolated stream: its literal subject sits under the
        # TWITCH_INGRESS wildcard, so a leftover makes any suite that
        # provisions the fleet streams against this broker fail on subject
        # overlap (and its memory reservation crowds out the 1 GiB
        # TWITCH_INGRESS spec).
        _ = Gnat.request(conn, "$JS.API.STREAM.DELETE." <> @stream, "")
      end
    end
  end

  # Idempotently provisions the isolated test stream with the NATS 2.14 batch
  # capabilities the production reconciler enables on the fleet streams.
  defp ensure_stream(conn) do
    config = %{
      name: @stream,
      subjects: ["twitch.ingress.event.standard"],
      retention: "limits",
      storage: "memory",
      discard: "old",
      max_bytes: 64 * 1024 * 1024,
      num_replicas: 1,
      allow_atomic: true,
      allow_batched: true
    }

    {:ok, %{body: body}} =
      Gnat.request(conn, "$JS.API.STREAM.CREATE." <> @stream, Ingress.JSON.encode(config))

    case Ingress.JSON.decode(body) do
      # name already in use
      {:ok, %{"error" => %{"err_code" => 10_058}}} -> :ok
      {:ok, %{"error" => error}} -> raise "stream create failed: #{inspect(error)}"
      {:ok, _} -> :ok
    end
  end

  defp purge_stream(conn) do
    {:ok, _} = Gnat.request(conn, "$JS.API.STREAM.PURGE." <> @stream, "")
    :ok
  end

  defp stream_messages(conn) do
    {:ok, %{body: body}} = Gnat.request(conn, "$JS.API.STREAM.INFO." <> @stream, "")
    {:ok, %{"state" => %{"messages" => messages}}} = Ingress.JSON.decode(body)
    messages
  end

  defp eventually(check, attempts \\ 100)

  defp eventually(check, attempts) do
    cond do
      check.() ->
        true

      attempts == 0 ->
        false

      true ->
        Process.sleep(10)
        eventually(check, attempts - 1)
    end
  end
end
