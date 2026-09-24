# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ScaleRpc do
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
