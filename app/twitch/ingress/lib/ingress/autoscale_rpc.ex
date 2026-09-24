# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AutoscaleRpc do
  use Ingress.RpcServer, log: "autoscale rpc"

  import Ingress.RpcServer, only: [decode_field: 3, scaler_reply: 1]

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
