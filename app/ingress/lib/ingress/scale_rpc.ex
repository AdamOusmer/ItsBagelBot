# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.ScaleRpc do
  @moduledoc """
  NATS request-reply handler for manual shard scaling.

  Subject: `NATS_SCALE_SUBJECT` (default `twitch.ingress.admin.shards.scale`).

  Request body (JSON):

      {"count": N}

  where `N` is the desired shard floor (integer >= 0). The effective target is
  clamped to `[min_shards, max_shards]` by `Ingress.ShardScaler.set_target/1`.

  Reply: full cluster snapshot from `Ingress.AdminRpc.snapshot/0` (same shape
  as the read-only admin endpoint), so the console can refresh state in a
  single round-trip.

  On bad input the reply is:

      {"error": "reason string"}

  The handler never crashes on malformed requests — decode errors and missing
  keys are caught and turned into error replies.
  """

  use Gnat.Server
  require Logger

  alias Ingress.{AdminRpc, JSON, ShardScaler}

  @impl true
  def request(%{body: body}) do
    reply =
      with {:ok, %{"count" => n}} when is_integer(n) <- JSON.decode(body),
           :ok <- ShardScaler.set_target(n) do
        AdminRpc.snapshot()
      else
        {:ok, _other} ->
          %{error: "body must be {\"count\": <integer>}"}

        # `Ingress.JSON.decode/1` reports the caught kind/reason (or trailing
        # data) as a pair, which is what separates a decode failure from the
        # scaler's own atom errors below.
        {:error, {_kind, _reason} = decode_error} ->
          %{error: "json decode error: #{inspect(decode_error)}"}

        {:error, :not_running} ->
          %{error: "shard_scaler not running"}

        {:error, reason} ->
          %{error: inspect(reason)}
      end

    {:reply, JSON.encode(reply)}
  end

  @impl true
  def error(_message, error) do
    Logger.error("scale rpc error: #{inspect(error)}")
    :ok
  end
end
