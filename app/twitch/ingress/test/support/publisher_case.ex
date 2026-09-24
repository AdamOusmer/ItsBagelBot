# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.PublisherCase do
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

  @spec start_fake_gnat(atom(), keyword()) :: pid()
  def start_fake_gnat(conn, opts \\ []) do
    {child_opts, start_opts} = Keyword.split(opts, [:id])
    args = [name: conn, test: self()] ++ start_opts

    ExUnit.Callbacks.start_supervised!({Ingress.FakeGnat, args}, child_opts)
  end

  @spec start_publisher(atom()) :: %{publisher: atom(), ctx: map()}
  def start_publisher(conn) do
    ExUnit.Callbacks.start_supervised!({Publisher, [index: 0, conn: conn]})
    :persistent_term.put({Publisher, :n}, 1)
    ExUnit.Callbacks.on_exit(fn -> :persistent_term.erase({Publisher, :n}) end)

    %{publisher: Publisher.process_name(0), ctx: :persistent_term.get({Publisher, :ctx, 0})}
  end

  @spec headers_map(keyword()) :: %{String.t() => String.t()}
  def headers_map(opts) do
    for [key, ": ", value, "\r\n"] <- Keyword.get(opts, :headers, []), into: %{} do
      {String.downcase(key), IO.iodata_to_binary(value)}
    end
  end

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
