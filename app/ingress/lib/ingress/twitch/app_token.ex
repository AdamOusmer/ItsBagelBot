defmodule Ingress.Twitch.AppToken do
  @moduledoc """
  Holds the Twitch app access token (client-credentials grant) and refreshes it
  before expiry. Conduit and shard management on Helix only needs the app
  token; no per-user token is involved in keeping shards bound.
  """

  use GenServer
  require Logger

  @token_url "https://id.twitch.tv/oauth2/token"
  # Refresh this long before the reported expiry.
  @expiry_slack_ms 60_000

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec get() :: {:ok, String.t()} | {:error, term()}
  def get, do: GenServer.call(__MODULE__, :get, 15_000)

  @doc "Drop the cached token (e.g. after Helix returned 401)."
  def invalidate, do: GenServer.cast(__MODULE__, :invalidate)

  @impl true
  def init(_opts), do: {:ok, %{token: nil, expires_at: 0}}

  @impl true
  def handle_call(:get, _from, state) do
    now = System.monotonic_time(:millisecond)

    if state.token && state.expires_at - @expiry_slack_ms > now do
      {:reply, {:ok, state.token}, state}
    else
      case fetch_token() do
        {:ok, token, expires_in_s} ->
          {:reply, {:ok, token}, %{token: token, expires_at: now + expires_in_s * 1000}}

        {:error, reason} ->
          Logger.error("twitch app token fetch failed: #{inspect(reason)}")
          {:reply, {:error, reason}, %{token: nil, expires_at: 0}}
      end
    end
  end

  @impl true
  def handle_cast(:invalidate, _state), do: {:noreply, %{token: nil, expires_at: 0}}

  defp fetch_token do
    form = [
      client_id: Ingress.Config.twitch_client_id(),
      client_secret: Ingress.Config.twitch_client_secret(),
      grant_type: "client_credentials"
    ]

    case Req.post(@token_url, form: form) do
      {:ok, %{status: 200, body: %{"access_token" => token, "expires_in" => expires_in}}} ->
        {:ok, token, expires_in}

      {:ok, %{status: status, body: body}} ->
        {:error, {:http, status, body}}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
