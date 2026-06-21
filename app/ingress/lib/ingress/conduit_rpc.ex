defmodule Ingress.ConduitRpc do
  @moduledoc """
  NATS request-reply handler that exposes the live conduit id held by
  `Ingress.ConduitManager`.

  Subject: `NATS_CONDUIT_SUBJECT` (default `bagel.rpc.ingress.conduit.get`).

  Request body is ignored (send `{}`).

  Reply on success:

      {"conduit_id": "<uuid>"}

  Reply on error:

      {"error": "<reason>"}

  Possible error strings:
    * `"conduit not ready"` - manager running but first reconcile not done yet.
    * `"conduit manager down"` - manager not registered in Horde.
    * `"conduit manager unresponsive"` - GenServer.call timed out or crashed.
  """

  use Gnat.Server
  require Logger

  @call_timeout_ms 5_000

  @impl true
  def request(%{body: _body}) do
    reply =
      case Horde.Registry.lookup(Ingress.Registry, :conduit_manager) do
        [{pid, _}] ->
          try do
            case GenServer.call(pid, :status, @call_timeout_ms) do
              %{conduit_id: id} when is_binary(id) ->
                %{conduit_id: id}

              %{conduit_id: nil} ->
                %{error: "conduit not ready"}

              _other ->
                %{error: "conduit not ready"}
            end
          catch
            :exit, _ -> %{error: "conduit manager unresponsive"}
          end

        [] ->
          %{error: "conduit manager down"}
      end

    {:reply, Jason.encode!(reply)}
  end

  @impl true
  def error(_message, error) do
    Logger.error("conduit rpc error: #{inspect(error)}")
    :ok
  end
end
