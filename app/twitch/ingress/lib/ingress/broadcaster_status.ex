# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.BroadcasterStatus do
  alias Ingress.Trace
  alias Platform.Sift

  @connection :gnat

  @spec lane_for(String.t()) :: {:ok, :premium | :standard | :drop} | {:error, term()}
  def lane_for(broadcaster_id) do
    request = Sift.encode_to_iodata!(%{broadcaster_id: broadcaster_id})

    Trace.span("broadcaster_status.request", fn ->
      with {:ok, %{body: body}} <- request_status(request),
           {:ok, reply} <- Sift.decode(body) do
        case reply do
          %{"banned" => true} -> {:ok, :drop}
          %{"runtime_allowed" => false} -> {:ok, :drop}
          %{"tier" => "premium"} -> {:ok, :premium}
          %{"error" => error} -> {:error, {:rpc, error}}
          _ -> {:ok, :standard}
        end
      else
        {:error, reason} -> {:error, reason}
      end
    end)
  end

  defp request_status(request) do
    Ingress.Rpc.request(@connection, Ingress.Config.broadcaster_status_subject(), request,
      receive_timeout: Ingress.Config.broadcaster_status_timeout_ms()
    )
  catch
    :exit, reason -> {:error, {:nats_down, reason}}
  end
end
