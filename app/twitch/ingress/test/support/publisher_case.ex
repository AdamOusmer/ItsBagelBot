# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.PublisherCase do
  @moduledoc """
  Case template for the publisher suites: the shard fixture (config overrides,
  fake connection, one collector, the shard-count `persistent_term`) plus the
  handful of probes every one of those suites had grown its own copy of.

  Facade over the fixture rather than a `setup` block of its own: each suite
  still writes its own `setup`, because they differ in which pieces they want
  (the admission suite deliberately starts NO connection, so its publishes
  never leave the VM). What they must not differ in is the teardown, which is
  where the copies had drifted — one of them restored three keys by hand and
  would silently leak a fourth.

  Every fixture registers its own `on_exit`, so teardown unwinds in reverse:
  the shard count is erased before the config it was started under is restored.

  The env save/restore and the polling probe moved to `Ingress.EnvCase`, which
  the suites outside this template needed too; they are re-imported here so
  publisher suites keep reaching them unqualified.
  """

  use ExUnit.CaseTemplate

  alias Ingress.Nats.Publisher

  using do
    quote do
      import Ingress.EnvCase
      import Ingress.PublisherCase

      alias Ingress.FakeGnat
      alias Ingress.Nats.Publisher
    end
  end

  @doc """
  Starts a supervised `Ingress.FakeGnat` registered under `conn`. Extra opts go
  straight through (`mode: :stalled`, `mode: :coalesce`, `id: ...`).
  """
  @spec start_fake_gnat(atom(), keyword()) :: pid()
  def start_fake_gnat(conn, opts \\ []) do
    {child_opts, start_opts} = Keyword.split(opts, [:id])
    args = [name: conn, test: self()] ++ start_opts

    ExUnit.Callbacks.start_supervised!({Ingress.FakeGnat, args}, child_opts)
  end

  @doc """
  Starts shard 0 of the publisher against `conn` and publishes the one-shard
  count the enqueue path reads, returning the test context both are reached
  through.
  """
  @spec start_publisher(atom()) :: %{publisher: atom(), ctx: map()}
  def start_publisher(conn) do
    ExUnit.Callbacks.start_supervised!({Publisher, [index: 0, conn: conn]})
    :persistent_term.put({Publisher, :n}, 1)
    ExUnit.Callbacks.on_exit(fn -> :persistent_term.erase({Publisher, :n}) end)

    %{publisher: Publisher.process_name(0), ctx: :persistent_term.get({Publisher, :ctx, 0})}
  end

  @doc """
  Decodes the headers off a captured publish. Gnat preps them into cowlib
  iodata before the connection call, so the test has to undo that to see them.
  """
  @spec headers_map(keyword()) :: %{String.t() => String.t()}
  def headers_map(opts) do
    for [key, ": ", value, "\r\n"] <- Keyword.get(opts, :headers, []), into: %{} do
      {String.downcase(key), IO.iodata_to_binary(value)}
    end
  end

  @doc """
  Rewrites every pending row's timestamp into the past so the next sweep treats
  it as expired, without waiting out the real ack timeout. Ages relative to the
  current monotonic clock — its absolute value is an arbitrary (typically
  negative) offset.
  """
  @spec age_pending_rows(map()) :: :ok
  def age_pending_rows(ctx) do
    expired = System.monotonic_time(:millisecond) - 10_000_000

    for row <- :ets.tab2list(ctx.table), do: :ets.insert(ctx.table, age_row(row, expired))

    :ok
  end

  defp age_row({id, :single, subject, json, attempts, _ts}, expired),
    do: {id, :single, subject, json, attempts, expired}

  defp age_row({id, :batch, entries, _ts}, expired), do: {id, :batch, entries, expired}
  defp age_row({id, :batch_hold, _ts}, expired), do: {id, :batch_hold, expired}
end
