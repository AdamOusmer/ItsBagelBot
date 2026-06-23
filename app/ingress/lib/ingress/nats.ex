defmodule Ingress.Nats do
  @moduledoc """
  Outbound NATS publishing of the twitch.ingress.* firehose. Fire-and-forget: if
  the connection is down (Gnat's supervisor is reconnecting), the message is
  dropped with a warning. We prefer drop over unbounded buffering.

  Publishes ride the BUS-account connection (`:gnat_bus`) because the
  twitch.ingress.event/status.* subjects are captured by the JetStream streams,
  which live in the shared BUS account; the RPC connection (`:gnat`) carries only
  the twitch_ingress account's request/reply traffic.
  """

  require Logger

  @connection :gnat_bus

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
