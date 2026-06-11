defmodule Ingress.BroadcasterStatus do
  @moduledoc """
  NATS request-reply client for broadcaster status.

  The ingress never reads MySQL directly: per the data-and-state ownership
  rules, broadcaster configuration belongs to its owning Go service, and we
  ask that service over NATS RPC. Only the `Ingress.BroadcasterCache` loader
  should call this; the hot chat path goes through the cache.

  Contract (subject from `NATS_BROADCASTER_STATUS_SUBJECT`):

      request:  {"broadcaster_id": "141981764"}
      reply:    {"broadcaster_id": "141981764", "tier": "premium"}

  Any `tier` other than `"premium"` maps to the standard lane, as does an
  unknown broadcaster.
  """

  require Logger

  @connection :gnat

  @spec lane_for(String.t()) :: {:ok, :premium | :standard} | {:error, term()}
  def lane_for(broadcaster_id) do
    request = Jason.encode!(%{broadcaster_id: broadcaster_id})

    with {:ok, %{body: body}} <-
           Gnat.request(@connection, Ingress.Config.broadcaster_status_subject(), request,
             receive_timeout: Ingress.Config.broadcaster_status_timeout_ms()
           ),
         {:ok, reply} <- Jason.decode(body) do
      case reply do
        %{"tier" => "premium"} -> {:ok, :premium}
        %{"error" => error} -> {:error, {:rpc, error}}
        _ -> {:ok, :standard}
      end
    else
      {:error, reason} -> {:error, reason}
    end
  catch
    # Gnat.request exits when the connection process is down; degrade instead.
    :exit, reason -> {:error, {:nats_down, reason}}
  end
end
