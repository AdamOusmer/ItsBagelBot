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

  use Ingress.RpcServer, log: "autoscale rpc"

  alias Ingress.{AdminRpc, JSON, ShardScaler}

  @impl Gnat.Server
  def request(%{body: body}) do
    reply =
      with {:ok, enabled} <- decode_field(body, "enabled", &is_boolean/1),
           :ok <- ShardScaler.set_autoscale(enabled) do
        AdminRpc.snapshot()
      else
        :invalid -> %{error: ~s(body must be {"enabled": <boolean>})}
        error -> scaler_reply(error)
      end

    {:reply, JSON.encode(reply)}
  end
end
