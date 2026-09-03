# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.HealthRpc do
  @moduledoc """
  Side-effect-free NATS request/reply endpoint used by the admin RPC latency
  panel. A reply proves the yt-ingress RPC connection, account route and
  consumer dispatcher are live without asking the session registry for a full
  snapshot.
  """

  use Gnat.Server
  require Logger

  @impl true
  def request(%{body: _body}) do
    {:reply, ~s({"service":"yt-ingress","ok":true})}
  end

  @impl true
  def error(_message, error) do
    Logger.error("health rpc error: #{inspect(error)}")
    :ok
  end
end
