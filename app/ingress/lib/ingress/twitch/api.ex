defmodule Ingress.Twitch.Api do
  @moduledoc """
  Helix calls the ingress needs: conduit lifecycle and shard binding.

  Twitch owns the Conduit (server-side state); these calls are how we reconcile
  our view against theirs. All calls use the app access token. A 401 drops the
  cached token so the next attempt re-authenticates.
  """

  require Logger

  alias Ingress.Config
  alias Ingress.Twitch.AppToken

  @helix "https://api.twitch.tv/helix"

  @doc """
  Returns `{:ok, conduit_id}` for the conduit this ingress owns, creating it if
  needed and growing its shard count up to the configured value.

  If `TWITCH_CONDUIT_ID` is set it is taken as authoritative; otherwise the
  first existing conduit for this client is reused, and one is created when
  none exists.
  """
  @spec ensure_conduit() :: {:ok, String.t()} | {:error, term()}
  def ensure_conduit do
    desired = Config.conduit_shard_count()

    with {:ok, conduits} <- list_conduits() do
      case pick_conduit(conduits) do
        {:ok, nil} ->
          create_conduit(desired)

        {:ok, %{"id" => id, "shard_count" => count}} when count < desired ->
          with :ok <- update_conduit(id, desired), do: {:ok, id}

        {:ok, %{"id" => id}} ->
          {:ok, id}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  # An unset conduit pin is a hard error: the conduit id is a shared contract
  # with outgress, so silently adopting the first conduit Twitch lists could
  # drift from the conduit outgress is enrolled into. A nil/empty pin means
  # the operator hasn't set TWITCH_CONDUIT_ID yet; fail loudly rather than
  # binding shards to an arbitrary conduit.
  defp pick_conduit(conduits) do
    case Config.twitch_conduit_id() do
      nil -> {:error, :conduit_id_unset}
      "" -> {:error, :conduit_id_unset}
      id ->
        case Enum.find(conduits, &(&1["id"] == id)) do
          nil -> {:error, {:pinned_conduit_missing, id}}
          conduit -> {:ok, conduit}
        end
    end
  end

  def list_conduits do
    with {:ok, body} <- request(:get, "/eventsub/conduits", nil) do
      {:ok, body["data"] || []}
    end
  end

  def create_conduit(shard_count) do
    with {:ok, %{"data" => [%{"id" => id} | _]}} <-
           request(:post, "/eventsub/conduits", %{shard_count: shard_count}) do
      Logger.info("created conduit #{id} with #{shard_count} shards")
      {:ok, id}
    end
  end

  def update_conduit(conduit_id, shard_count) do
    with {:ok, _} <-
           request(:patch, "/eventsub/conduits", %{id: conduit_id, shard_count: shard_count}) do
      Logger.info("resized conduit #{conduit_id} to #{shard_count} shards")
      :ok
    end
  end

  @doc """
  Binds a WebSocket `session_id` to `shard_id` on the conduit. This is the call
  a shard session makes after receiving `session_welcome` on a fresh socket.
  """
  @spec assign_shard(String.t(), non_neg_integer(), String.t()) :: :ok | {:error, term()}
  def assign_shard(conduit_id, shard_id, session_id) do
    payload = %{
      conduit_id: conduit_id,
      shards: [
        %{
          id: to_string(shard_id),
          transport: %{method: "websocket", session_id: session_id}
        }
      ]
    }

    case request(:patch, "/eventsub/conduits/shards", payload) do
      {:ok, %{"errors" => [_ | _] = errors}} -> {:error, {:shard_errors, errors}}
      {:ok, _body} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp request(method, path, json) do
    with {:ok, token} <- AppToken.get() do
      opts =
        [
          url: @helix <> path,
          method: method,
          headers: [
            {"client-id", Config.twitch_client_id()},
            {"authorization", "Bearer " <> token}
          ]
        ] ++ if(json, do: [json: json], else: [])

      case Req.request(opts) do
        {:ok, %{status: status, body: body}} when status in 200..299 ->
          {:ok, body}

        {:ok, %{status: 401, body: body}} ->
          AppToken.invalidate()
          {:error, {:http, 401, body}}

        {:ok, %{status: status, body: body}} ->
          {:error, {:http, status, body}}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end
end
