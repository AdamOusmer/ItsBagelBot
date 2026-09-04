# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AutoscaleRpc do
  @moduledoc """
  NATS request-reply handler for toggling the load-based autoscaler.

  Subject: `NATS_AUTOSCALE_SUBJECT` (default
  `twitch.ingress.admin.shards.autoscale`).

  Request body (JSON):

      {"enabled": true}
      {"enabled": false}

  Reply: full cluster snapshot from `Ingress.AdminRpc.snapshot/0` (same shape
  as the read-only admin endpoint), so the console can refresh state in a
  single round-trip.

  On bad input the reply is:

      {"error": "reason string"}

  The handler never crashes on malformed requests.
  """

  use Gnat.Server
  require Logger

  alias Ingress.{AdminRpc, JSON, ShardScaler}

  @impl true
  def request(%{body: body}) do
    reply =
      with {:ok, %{"enabled" => enabled}} when is_boolean(enabled) <- JSON.decode(body),
           :ok <- ShardScaler.set_autoscale(enabled) do
        AdminRpc.snapshot()
      else
        {:ok, _other} ->
          %{error: "body must be {\"enabled\": <boolean>}"}

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
    Logger.error("autoscale rpc error: #{inspect(error)}")
    :ok
  end
end
