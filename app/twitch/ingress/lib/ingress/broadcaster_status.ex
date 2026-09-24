# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.BroadcasterStatus do
  alias Ingress.{JSON, Trace}

  @connection :gnat

  @spec lane_for(String.t()) :: {:ok, :premium | :standard | :drop} | {:error, term()}
  def lane_for(broadcaster_id) do
    request = JSON.encode(%{broadcaster_id: broadcaster_id})

    Trace.span("broadcaster_status.request", [dependency: "nats"], fn ->
      result =
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

      Trace.add_span_attributes(result: Trace.result(result))
      result
    end)
  end

  defp request_status(request) do
    Ingress.Rpc.request(@connection, Ingress.Config.broadcaster_status_subject(), request,
      receive_timeout: Ingress.Config.broadcaster_status_timeout_ms(),
      headers: Trace.trace_headers()
    )
  catch
    :exit, reason -> {:error, {:nats_down, reason}}
  end
end
