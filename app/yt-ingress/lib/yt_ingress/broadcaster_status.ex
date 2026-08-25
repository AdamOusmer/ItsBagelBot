# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.BroadcasterStatus do
  @moduledoc """
  NATS request-reply client for broadcaster status.

  The ingress never reads MySQL directly: per the data-and-state ownership
  rules, broadcaster configuration belongs to its owning Go service, and we
  ask that service over NATS RPC. Only the `YtIngress.BroadcasterCache` loader
  should call this; the hot event path goes through the cache.

  Contract (subject from `NATS_BROADCASTER_STATUS_SUBJECT`), identical to the
  Twitch ingress so one Go endpoint serves both:

      request:  {"broadcaster_id": "UC_x5XG1OV2P6uZZ5FSM9Ttw"}
      reply:    {"broadcaster_id": "UC_x5XG1OV2P6uZZ5FSM9Ttw", "tier": "premium"}

  Any `tier` other than `"premium"` maps to the standard lane, as does an
  unknown broadcaster — which today is every YouTube channel, until the owning
  service learns about them. A `"banned"` flag wins over everything: a banned
  broadcaster resolves to `:drop` so the ingress discards their traffic.
  """

  alias YtIngress.{Config, JSON, Rpc}

  @connection :gnat

  @spec lane_for(String.t()) :: {:ok, :premium | :standard | :drop} | {:error, term()}
  def lane_for(broadcaster_id) do
    request = JSON.encode(%{broadcaster_id: broadcaster_id})

    with {:ok, %{body: body}} <- request_status(request),
         {:ok, reply} <- JSON.decode(body) do
      case reply do
        %{"banned" => true} -> {:ok, :drop}
        %{"tier" => "premium"} -> {:ok, :premium}
        %{"error" => error} -> {:error, {:rpc, error}}
        _ -> {:ok, :standard}
      end
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp request_status(request) do
    Rpc.request(@connection, Config.broadcaster_status_subject(), request,
      receive_timeout: Config.broadcaster_status_timeout_ms()
    )
  catch
    # Gnat.request exits when the connection process is down; degrade instead.
    :exit, reason -> {:error, {:nats_down, reason}}
  end
end
