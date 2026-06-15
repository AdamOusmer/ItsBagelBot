defmodule Ingress.ScaleRpc do
  @moduledoc """
  NATS request-reply handler for manual shard scaling.

  Subject: `NATS_SCALE_SUBJECT` (default `twitch.ingress.admin.shards.scale`).

  Request body (JSON):

      {"count": N}

  where `N` is the desired shard floor (integer >= 0). The effective target is
  clamped to `[min_shards, max_shards]` by `Ingress.ShardScaler.set_target/1`.

  Reply: full cluster snapshot from `Ingress.AdminRpc.snapshot/0` (same shape
  as the read-only admin endpoint), so the console can refresh state in a
  single round-trip.

  On bad input the reply is:

      {"error": "reason string"}

  The handler never crashes on malformed requests — decode errors and missing
  keys are caught and turned into error replies.
  """

  use Gnat.Server
  require Logger

  alias Ingress.{AdminRpc, ShardScaler}

  @impl true
  def request(%{body: body}) do
    reply =
      with {:ok, %{"count" => n}} when is_integer(n) <- Jason.decode(body),
           :ok <- ShardScaler.set_target(n) do
        AdminRpc.snapshot()
      else
        {:ok, _other} ->
          %{error: "body must be {\"count\": <integer>}"}

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
    Logger.error("scale rpc error: #{inspect(error)}")
    :ok
  end
end
