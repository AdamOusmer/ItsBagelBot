defmodule Ingress.HealthRpc do
  @moduledoc """
  Side-effect-free NATS request/reply endpoint used by the admin RPC latency
  panel. A reply proves the ingress RPC connection, account route and consumer
  dispatcher are live without asking the shard registry for a full snapshot.
  """

  use Gnat.Server
  require Logger

  @impl true
  def request(%{body: _body}) do
    {:reply, ~s({"service":"ingress","ok":true})}
  end

  @impl true
  def error(_message, error) do
    Logger.error("health rpc error: #{inspect(error)}")
    :ok
  end
end
