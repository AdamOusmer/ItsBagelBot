defmodule Ingress.Squash do
  @moduledoc """
  Coalesces identical non-command chat so the worker keeps the full reputation
  and campaign signal at a fraction of the event count.

  The `!` filter is gone, so every chat line now flows to the worker for the
  automod. A raid or copypasta means the same text lands on a channel from many
  chatters (or one chatter repeating). Dropping those duplicates would blind the
  worker's per-user reputation and cross-user campaign detection, and forwarding
  each is wasteful. So instead:

    * the FIRST occurrence of a `{broadcaster, trimmed-text}` is published
      immediately as a normal `channel.chat.message` (zero added latency for the
      common unique message, and an instant content look for the automod);
    * every later identical line within the window is buffered as just its
      SENDER, and the window is flushed into ONE folded `channel.chat.message`
      carrying every duplicate sender. `M` distinct users on identical text is
      exactly the campaign primitive, delivered pre-assembled, and it rides the
      normal premium/standard lane like any other chat event.

  Commands (`!followage`, custom commands with integrations) and special users
  never reach here: a repeated command is a legitimate second invocation the
  worker gates with its own cooldown, so those are published individually.

  Per-pod state is sufficient: a channel's chat is owned by one shard on one
  node, so its duplicates land on the same table. The first-check is an atomic
  `:ets.insert_new` from the calling process (lock-free, no GenServer hop on the
  unique-message hot path); only actual duplicates cast to the GenServer, which
  accumulates senders and flushes on the sweep or a size cap.
  """

  use GenServer
  require Logger

  alias Ingress.{Config, Metrics, Nats}

  @keys_table __MODULE__.Keys

  @type base :: %{
          broadcaster_user_id: String.t(),
          broadcaster_user_login: String.t() | nil,
          lane: :premium | :standard,
          text: String.t()
        }
  @type sender :: %{
          chatter_user_id: String.t() | nil,
          chatter_user_login: String.t() | nil,
          msg_id: String.t() | nil,
          ts: term(),
          badges: term()
        }

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc """
  Records one plain-chat line. Returns `:first` when this is the opening
  occurrence in the window (the caller publishes it normally), or `:buffered`
  when it is a duplicate (the caller drops it; it will re-surface inside the
  cohort event).
  """
  @spec observe(base(), sender(), GenServer.server()) :: :first | :buffered
  def observe(base, sender, server \\ __MODULE__) do
    key = {base.broadcaster_user_id, String.trim(base.text)}

    if :ets.insert_new(@keys_table, {key, now_ms() + window_ms()}) do
      :first
    else
      GenServer.cast(server, {:dup, key, base, sender})
      :buffered
    end
  rescue
    # Table missing (Squash not started, e.g. in a unit test that exercises the
    # pipeline alone): fail open and publish rather than lose the message.
    ArgumentError -> :first
  end

  @impl true
  def init(opts) do
    :ets.new(@keys_table, [
      :set,
      :public,
      :named_table,
      read_concurrency: true,
      write_concurrency: true
    ])

    :persistent_term.put(
      {__MODULE__, :window_ms},
      Keyword.get(opts, :window_ms) || Config.squash_window_ms()
    )

    state = %{
      cohorts: %{},
      max_senders: Keyword.get(opts, :max_senders) || Config.squash_max_senders(),
      sweep_ms: Keyword.get(opts, :sweep_ms) || Config.squash_sweep_ms(),
      publish: Keyword.get(opts, :publish, &__MODULE__.publish_cohort/2)
    }

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:ok, state}
  end

  @impl true
  def handle_cast({:dup, key, base, sender}, state) do
    cohorts =
      Map.update(state.cohorts, key, %{base: base, senders: [sender], count: 1}, fn c ->
        %{c | senders: [sender | c.senders], count: c.count + 1}
      end)

    cohort = Map.fetch!(cohorts, key)

    # A cohort that hits the cap flushes early: bounds the event size and hands
    # the worker a raid cohort without waiting for the window to close.
    if cohort.count >= state.max_senders do
      emit(cohort, state)
      :ets.delete(@keys_table, key)
      {:noreply, %{state | cohorts: Map.delete(cohorts, key)}}
    else
      {:noreply, %{state | cohorts: cohorts}}
    end
  end

  @impl true
  def handle_info(:sweep, state) do
    now = now_ms()
    # Every key whose window has closed. Keys with no cohort are unique messages
    # (first published, no duplicates) and are just cleaned up; keys with a
    # cohort are flushed into one event.
    expired = :ets.select(@keys_table, [{{:"$1", :"$2"}, [{:"=<", :"$2", now}], [:"$1"]}])

    cohorts =
      Enum.reduce(expired, state.cohorts, fn key, acc ->
        :ets.delete(@keys_table, key)

        case Map.pop(acc, key) do
          {nil, acc} -> acc
          {cohort, acc} -> emit(cohort, state) && acc
        end
      end)

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:noreply, %{state | cohorts: cohorts}}
  end

  # Build and publish the cohort event onto the broadcaster's lane. It carries
  # only the DUPLICATE senders; the first occurrence already rode a normal
  # channel.chat.message, so the worker aggregates the two by text (and dedups
  # by msg_id) with no double count. The cohort's own msg_id is the earliest
  # buffered duplicate's — never published individually, so it is free to
  # anchor the cohort's broker-side dedup id.
  defp emit(%{base: base, senders: senders, count: count}, state) do
    distinct = senders |> Enum.map(& &1.chatter_user_id) |> Enum.uniq() |> length()
    ordered = Enum.reverse(senders)

    message = %{
      type: "channel.chat.message",
      lane: base.lane,
      broadcaster_user_id: base.broadcaster_user_id,
      broadcaster_user_login: base.broadcaster_user_login,
      text: base.text,
      msg_id: List.first(ordered).msg_id,
      senders: ordered,
      count: count,
      distinct_users: distinct
    }

    Metrics.count("Cohorts/Emitted")
    Metrics.count("Cohorts/Senders", count)
    state.publish.(Config.lane_subject(base.lane), message)
    true
  end

  @doc false
  # Default cohort publisher: JetStream-acked with a cohort-scoped dedup id,
  # run on a supervised task so the squash sweep never blocks on the broker's
  # PubAck (a cohort carries many senders — losing one silently would blind
  # the automod's campaign signal, so fire-and-forget is not acceptable here).
  def publish_cohort(subject, message) do
    Task.Supervisor.start_child(Ingress.PublishSupervisor, fn ->
      Nats.publish_acked(subject, message, cohort_dedup_id(message))
    end)
  end

  defp cohort_dedup_id(%{msg_id: msg_id}) when is_binary(msg_id), do: "#{msg_id}:cohort"
  defp cohort_dedup_id(_message), do: nil

  defp window_ms, do: :persistent_term.get({__MODULE__, :window_ms}, 2_000)
  defp now_ms, do: System.monotonic_time(:millisecond)
end
