# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.NatsCohortIntegrationTest do
  use Ingress.PublisherCase, async: false

  @idx_pending 1

  @moduletag :integration

  @stream "ELIXIR_BATCH_TEST"

  test "Gnat-managed connection receives individual PubAcks from NATS" do
    with_integration_publisher([publish_wire: :single], fn conn, ctx ->
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":1})) == :ok
      assert Publisher.enqueue("twitch.ingress.event.standard", ~s({"n":2})) == :ok

      assert eventually(fn -> :atomics.get(ctx.counter, @idx_pending) == 0 end)
      assert stream_messages(conn) == 2
    end)
  end

  test "atomic wire lands whole cohorts with one commit PubAck" do
    with_integration_publisher([publish_wire: :atomic, publish_batch_size: 3], fn conn, ctx ->
      for n <- 1..3 do
        assert Publisher.enqueue(
                 "twitch.ingress.event.standard",
                 ~s({"n":#{n}})
               ) == :ok
      end

      assert eventually(fn -> :atomics.get(ctx.counter, @idx_pending) == 0 end)
      assert stream_messages(conn) == 3
      assert :ets.info(ctx.table, :size) == 0
    end)
  end

  test "atomic wire stores repeated cohorts because broker dedup is disabled" do
    with_integration_publisher([publish_wire: :atomic, publish_batch_size: 3], fn conn, ctx ->
      replay = fn ->
        for n <- 1..3 do
          assert Publisher.enqueue(
                   "twitch.ingress.event.standard",
                   ~s({"n":#{n}})
                 ) == :ok
        end

        assert eventually(fn -> :atomics.get(ctx.counter, @idx_pending) == 0 end)
      end

      replay.()
      assert stream_messages(conn) == 3
      replay.()
      assert stream_messages(conn) == 6
    end)
  end

  defp with_integration_publisher(overrides, run) do
    port = System.get_env("NATS_INTEGRATION_PORT")

    if is_nil(port) do
      :ok
    else
      port = String.to_integer(port)
      conn = :gnat_batch_integration

      put_env(Keyword.merge([publish_batch_size: 2, publish_batch_wait_ms: 100], overrides))

      {:ok, gnat} = Gnat.start_link(%{host: ~c"127.0.0.1", port: port}, name: conn)
      ensure_stream(conn)
      purge_stream(conn)

      on_exit(fn -> if Process.alive?(gnat), do: GenServer.stop(gnat) end)
      %{ctx: ctx} = start_publisher(conn)

      try do
        run.(conn, ctx)
      after
        _ = Gnat.request(conn, "$JS.API.STREAM.DELETE." <> @stream, "")
      end
    end
  end

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
end
