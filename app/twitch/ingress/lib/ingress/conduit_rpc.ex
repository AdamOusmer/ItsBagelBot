# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ConduitRpc do
  use Ingress.RpcServer, log: "conduit rpc"

  alias Ingress.{JSON, Singleton}

  @call_timeout_ms 5_000

  @impl true
  def request(%{body: _body}) do
    reply =
      :conduit_manager
      |> Singleton.call(:status, @call_timeout_ms, &unreachable/1)
      |> conduit_reply()

    {:reply, JSON.encode(reply)}
  end

  defp unreachable(:down), do: %{error: "conduit manager down"}
  defp unreachable({:unresponsive, _pid}), do: %{error: "conduit manager unresponsive"}

  defp conduit_reply(%{conduit_id: id}) when is_binary(id), do: %{conduit_id: id}
  defp conduit_reply(%{error: _reason} = reply), do: reply
  defp conduit_reply(_not_ready), do: %{error: "conduit not ready"}
end
