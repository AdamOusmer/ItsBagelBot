defmodule Ingress.Nats do
  @moduledoc """
  Outbound NATS publishing. Fire-and-forget: if the connection is down (Gnat's
  supervisor is reconnecting), the message is dropped with a warning. We
  prefer drop over unbounded buffering.
  """

  require Logger

  @connection :gnat

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    json = Jason.encode!(payload)

    case Process.whereis(@connection) do
      nil ->
        Logger.warning("NATS connection down; dropping message on #{subject}")
        Ingress.Metrics.count("Nats/PublishDropped")
        {:error, :not_connected}

      _pid ->
        Gnat.pub(@connection, subject, json)
    end
  end
end
