# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

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

  use Ingress.RpcServer, log: "scale rpc"

  import Ingress.RpcServer, only: [decode_field: 3, scaler_reply: 1]

  alias Ingress.{AdminRpc, JSON, ShardScaler}

  @impl Gnat.Server
  def request(%{body: body}) do
    reply =
      with {:ok, n} <- decode_field(body, "count", &is_integer/1),
           :ok <- ShardScaler.set_target(n) do
        AdminRpc.snapshot()
      else
        :invalid -> %{error: ~s(body must be {"count": <integer>})}
        error -> scaler_reply(error)
      end

    {:reply, JSON.encode(reply)}
  end
end
