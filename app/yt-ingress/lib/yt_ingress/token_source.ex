# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.TokenSource do
  @moduledoc """
  Resolves the credential that authorizes one channel's live chat reads and
  broadcast discovery calls.

  Two modes:

    * Static API key (`YOUTUBE_API_KEY`, development): one project-wide
      credential for every channel. streamList accepts an API key when the
      chat is public; production runs OAuth instead because linked channels
      may restrict their chats to members.

    * Per-channel OAuth access token over NATS RPC (production). The ingress
      never stores Google refresh tokens: per the data-and-state ownership
      rules they belong to the users service, which leases short-lived access
      tokens out on request.

  Contract (subject from `NATS_YT_TOKEN_SUBJECT`):

      request:  {"channel_id": "UC_x5XG1OV2P6uZZ5FSM9Ttw"}
      reply:    {"channel_id": "...", "access_token": "...", "expires_at": 1755880000}

  `expires_at` is unix seconds; entries are cached until
  `expires_at - token_refresh_margin_seconds`. A mid-stream auth failure calls
  `refresh/1` once before the session gives up, so a rotated token heals a
  live stream without a restart.
  """

  use GenServer
  require Logger

  alias YtIngress.{JSON, Metrics, Rpc}
  alias YtIngress.Config.YouTube, as: YouTubeConfig

  @connection :gnat

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @type cred :: {:api_key, String.t()} | {:bearer, String.t()}

  @spec authorize(String.t() | nil) :: {:ok, cred()} | {:error, term()}
  def authorize(nil) do
    # A pinned live chat has no channel identity; only the static API-key mode
    # can authorize it.
    case api_key_cred() do
      {:ok, cred} -> {:ok, cred}
      :error -> {:error, :no_channel_credential}
    end
  end

  def authorize(channel_id) do
    with :error <- api_key_cred() do
      GenServer.call(__MODULE__, {:token, channel_id})
    end
  end

  @doc "Drops any cached token for the channel and fetches a fresh one."
  @spec refresh(String.t()) :: {:ok, cred()} | {:error, term()}
  def refresh(channel_id), do: GenServer.call(__MODULE__, {:refresh, channel_id})

  @doc "gRPC call metadata carrying the credential."
  @spec grpc_metadata(cred()) :: [{String.t(), String.t()}]
  def grpc_metadata({:api_key, key}), do: [{"x-goog-api-key", key}]
  def grpc_metadata({:bearer, token}), do: [{"authorization", "Bearer " <> token}]

  @impl true
  def init(_opts), do: {:ok, %{tokens: %{}, inflight: %{}, refs: %{}}}

  @impl true
  def handle_call({:token, channel_id}, from, state) do
    case cached_token(state.tokens, channel_id) do
      {:ok, token} ->
        {:reply, {:ok, {:bearer, token}}, state}

      :stale ->
        fetch_and_reply(channel_id, from, state)
    end
  end

  def handle_call({:refresh, channel_id}, from, state) do
    fetch_and_reply(channel_id, from, %{state | tokens: Map.delete(state.tokens, channel_id)})
  end

  # Misses run through a supervised task so an RPC outage costs the caller its
  # timeout, never the TokenSource itself. Concurrent callers for the same
  # channel join the in-flight fetch rather than stampeding the users RPC.
  defp fetch_and_reply(channel_id, from, state) do
    case Map.get(state.inflight, channel_id) do
      {ref, waiters} ->
        {:noreply,
         %{state | inflight: Map.put(state.inflight, channel_id, {ref, [from | waiters]})}}

      nil ->
        task =
          Task.Supervisor.async_nolink(YtIngress.TokenSource.TaskSupervisor, fn ->
            request_token(channel_id)
          end)

        {:noreply,
         %{
           state
           | inflight: Map.put(state.inflight, channel_id, {task.ref, [from]}),
             refs: Map.put(state.refs, task.ref, channel_id)
         }}
    end
  end

  @impl true
  def handle_info({ref, result}, state) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    finish_inflight(ref, result, state)
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    finish_inflight(ref, {:error, {:task, reason}}, state)
  end

  defp finish_inflight(ref, result, state) do
    case Map.pop(state.refs, ref) do
      {nil, _} ->
        {:noreply, state}

      {channel_id, refs} ->
        {entry, inflight} = Map.pop(state.inflight, channel_id)
        waiters =
          case entry do
            {_ref, froms} -> froms
            nil -> []
          end

        case result do
          {:ok, token, expires_at} ->
            Enum.each(waiters, &GenServer.reply(&1, {:ok, {:bearer, token}}))
            {:noreply,
             %{state | tokens: Map.put(state.tokens, channel_id, {token, expires_at}), inflight: inflight, refs: refs}}

          {:error, reason} ->
            Logger.warning("token fetch failed for #{channel_id}: #{inspect(reason)}")
            Metrics.count("Token/FetchErrors")
            Enum.each(waiters, &GenServer.reply(&1, {:error, reason}))
            {:noreply, %{state | inflight: inflight, refs: refs}}
        end
    end
  end

  defp cached_token(tokens, channel_id) do
    case Map.get(tokens, channel_id) do
      {token, expires_at} ->
        margin = YouTubeConfig.token_refresh_margin_seconds()

        if now_unix() < expires_at - margin do
          {:ok, token}
        else
          :stale
        end

      nil ->
        :stale
    end
  end

  defp request_token(channel_id) do
    request = JSON.encode(%{channel_id: channel_id})

    with {:ok, %{body: body}} <-
           Rpc.request(@connection, YouTubeConfig.token_subject(), request,
             receive_timeout: YouTubeConfig.token_timeout_ms()
           ),
         {:ok, %{"access_token" => token, "expires_at" => expires_at}} when is_binary(token) <-
           JSON.decode(body) do
      {:ok, token, expires_at}
    else
      {:ok, %{status: status}} -> {:error, {:rpc_status, status}}
      {:ok, reply} -> {:error, {:bad_reply, reply}}
      {:error, reason} -> {:error, reason}
    end
  catch
    :exit, reason -> {:error, {:nats_down, reason}}
  end

  defp api_key_cred do
    case YouTubeConfig.api_key() do
      key when is_binary(key) and key != "" -> {:ok, {:api_key, key}}
      _ -> :error
    end
  end

  defp now_unix, do: System.system_time(:second)
end
