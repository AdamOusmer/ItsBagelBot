defmodule Ingress.AutoscaleRpc do
  @moduledoc """
  NATS request-reply handler for toggling the load-based autoscaler.

  Subject: `NATS_AUTOSCALE_SUBJECT` (default
  `twitch.ingress.admin.shards.autoscale`).

  Request body (JSON):

      {"enabled": true}
      {"enabled": false}

  Reply: full cluster snapshot from `Ingress.AdminRpc.snapshot/0` (same shape
  as the read-only admin endpoint), so the console can refresh state in a
  single round-trip.

  On bad input the reply is:

      {"error": "reason string"}

  The handler never crashes on malformed requests.
  """

  use Gnat.Server
  require Logger

  alias Ingress.{AdminRpc, ShardScaler}

  @impl true
  def request(%{body: body}) do
    reply =
      with {:ok, %{"enabled" => enabled}} when is_boolean(enabled) <- Jason.decode(body),
           :ok <- ShardScaler.set_autoscale(enabled) do
        AdminRpc.snapshot()
      else
        {:ok, _other} ->
          %{error: "body must be {\"enabled\": <boolean>}"}

        {:error, %Jason.DecodeError{} = e} ->
          %{error: "json decode error: #{Exception.message(e)}"}

        {:error, :not_running} ->
          %{error: "shard_scaler not running"}

        {:error, reason} ->
          %{error: inspect(reason)}
      end

    {:reply, Jason.encode!(reply)}
  end

  @impl true
  def error(_message, error) do
    Logger.error("autoscale rpc error: #{inspect(error)}")
    :ok
  end
end
