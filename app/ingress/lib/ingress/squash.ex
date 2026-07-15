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
  node. The first-check is an atomic `:ets.insert_new` from the calling process;
  only actual duplicates message a cohort owner. Production runs one owner per
  online scheduler, partitioned by `{broadcaster, text}`, so unrelated floods
  never converge on one GenServer. The production chat API delays allocating
  cohort and sender maps until the ETS insert proves the line is a duplicate.
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

    with %{table: table, server: owner, window_ms: window_ms} <- context(server, key) do
      do_observe(table, owner, key, {:prepared, base, sender}, window_ms)
    else
      nil -> :first
    end
  rescue
    # Table missing (Squash not started, e.g. in a unit test that exercises the
    # pipeline alone): fail open and publish rather than lose the message.
    ArgumentError -> :first
  end

  @doc """
  Hot-path chat observation that defers cohort-map allocation until a duplicate
  actually exists. Unique chat stores only its compact ETS generation row.
  """
  @spec observe_chat(:premium | :standard, map(), String.t(), map()) :: :first | :buffered
  def observe_chat(lane, event, text, meta) do
    key = {event["broadcaster_user_id"], String.trim(text)}

    with %{table: table, server: owner, window_ms: window_ms} <- context(__MODULE__, key) do
      do_observe(table, owner, key, {:chat, lane, event, text, meta}, window_ms)
    else
      nil -> :first
    end
  rescue
    ArgumentError -> :first
  end

  @impl true
  def init(opts) do
    name = Keyword.get(opts, :name, __MODULE__)
    table = Keyword.get(opts, :table, @keys_table)
    window_ms = Keyword.get(opts, :window_ms) || Config.squash_window_ms()

    :ets.new(table, [
      :set,
      :public,
      :named_table,
      read_concurrency: true,
      write_concurrency: true,
      decentralized_counters: true
    ])

    :persistent_term.put({__MODULE__, :ctx, name}, %{
      table: table,
      server: name,
      window_ms: window_ms
    })

    state = %{
      name: name,
      table: table,
      cohorts: %{},
      max_senders: Keyword.get(opts, :max_senders) || Config.squash_max_senders(),
      sweep_ms: Keyword.get(opts, :sweep_ms) || Config.squash_sweep_ms(),
      publish: Keyword.get(opts, :publish, &__MODULE__.publish_cohort/2)
    }

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:ok, state}
  end

  @impl true
  def handle_cast({:dup, key, generation, base, sender}, state) do
    cohort_key = {key, generation}

    # The sweep may have closed this generation after observe/3 read the ETS
    # row but before this cast arrived. Emit that sender as a one-item cohort
    # instead of creating an orphan that can never be swept.
    if current_generation?(state.table, key, generation) do
      collect_duplicate(cohort_key, key, generation, base, sender, state)
    else
      emit(%{base: base, senders: [sender], count: 1}, state)
      {:noreply, state}
    end
  end

  @impl true
  def handle_call({:expire, key, expires_at, generation}, _from, state) do
    cohort_key = {key, generation}

    state =
      case :ets.lookup(state.table, key) do
        [{^key, ^expires_at, ^generation}] ->
          :ets.delete(state.table, key)

          case Map.pop(state.cohorts, cohort_key) do
            {nil, cohorts} -> %{state | cohorts: cohorts}
            {cohort, cohorts} -> emit(cohort, state) && %{state | cohorts: cohorts}
          end

        _ ->
          state
      end

    {:reply, :ok, state}
  end

  defp collect_duplicate(cohort_key, key, generation, base, sender, state) do
    cohorts =
      Map.update(state.cohorts, cohort_key, %{base: base, senders: [sender], count: 1}, fn c ->
        %{c | senders: [sender | c.senders], count: c.count + 1}
      end)

    cohort = Map.fetch!(cohorts, cohort_key)

    # A cohort that hits the cap flushes early: bounds the event size and hands
    # the worker a raid cohort without waiting for the window to close.
    if cohort.count >= state.max_senders do
      emit(cohort, state)
      delete_generation(state.table, key, generation)
      {:noreply, %{state | cohorts: Map.delete(cohorts, cohort_key)}}
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
    expired =
      :ets.select(state.table, [
        {{:"$1", :"$2", :"$3"}, [{:"=<", :"$2", now}], [{{:"$1", :"$2", :"$3"}}]}
      ])

    cohorts =
      Enum.reduce(expired, state.cohorts, fn {key, expires_at, generation}, acc ->
        # The exact generation check prevents an old sweep result from erasing
        # a newer window installed while the select was running.
        if current_entry?(state.table, key, expires_at, generation) do
          :ets.delete(state.table, key)

          case Map.pop(acc, {key, generation}) do
            {nil, acc} -> acc
            {cohort, acc} -> emit(cohort, state) && acc
          end
        else
          acc
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
  # identify the cohort to downstream consumers.
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
    state.publish.(Config.hot_lane_subject(base.lane), message)
    true
  end

  @doc false
  # Default cohort publisher: admission returns after Gnat accepts the writes
  # and reconciles each PubAck separately, so spawning one Task per cohort
  # would only add process churn. A cohort carries many senders and is never
  # fire-and-forget.
  def publish_cohort(subject, message) do
    Nats.publish_acked(subject, message)
  end

  @impl true
  def terminate(_reason, state) do
    :persistent_term.erase({__MODULE__, :ctx, state.name})
    :ok
  end

  defp context(__MODULE__, key) do
    case :persistent_term.get({__MODULE__, :partitions}, nil) do
      nil ->
        :persistent_term.get({__MODULE__, :ctx, __MODULE__}, nil)

      names ->
        :persistent_term.get(
          {__MODULE__, :ctx, elem(names, :erlang.phash2(key, tuple_size(names)))},
          nil
        )
    end
  end

  defp context(server, _key), do: :persistent_term.get({__MODULE__, :ctx, server}, nil)

  defp do_observe(table, server, key, duplicate, window_ms) do
    now = now_ms()
    # References are scheduler-local and only need to distinguish successive
    # generations of this key; a globally monotonic integer adds needless
    # contention to the unique-chat hot path.
    generation = make_ref()
    entry = {key, now + window_ms, generation}

    if :ets.insert_new(table, entry) do
      :first
    else
      case :ets.lookup(table, key) do
        [{^key, expires_at, current_generation}] when expires_at > now ->
          {base, sender} = duplicate_parts(duplicate)
          GenServer.cast(server, {:dup, key, current_generation, base, sender})
          :buffered

        [{^key, expires_at, current_generation}] ->
          # Rotation is serialized with duplicate casts and sweeps only when a
          # caller reaches an already-expired row. The unique-message hot path
          # remains entirely caller-side.
          GenServer.call(server, {:expire, key, expires_at, current_generation})
          do_observe(table, server, key, duplicate, window_ms)

        [] ->
          do_observe(table, server, key, duplicate, window_ms)
      end
    end
  end

  defp duplicate_parts({:prepared, base, sender}), do: {base, sender}

  defp duplicate_parts({:chat, lane, event, text, meta}) do
    base = %{
      broadcaster_user_id: event["broadcaster_user_id"],
      broadcaster_user_login: event["broadcaster_user_login"],
      lane: lane,
      text: text
    }

    sender = %{
      chatter_user_id: event["chatter_user_id"],
      chatter_user_login: event["chatter_user_login"],
      msg_id: meta.msg_id,
      ts: meta.ts,
      badges: event["badges"]
    }

    {base, sender}
  end

  defp current_generation?(table, key, generation) do
    case :ets.lookup(table, key) do
      [{^key, _expires_at, ^generation}] -> true
      _ -> false
    end
  end

  defp current_entry?(table, key, expires_at, generation) do
    case :ets.lookup(table, key) do
      [{^key, ^expires_at, ^generation}] -> true
      _ -> false
    end
  end

  defp delete_generation(table, key, generation) do
    case :ets.lookup(table, key) do
      [{^key, expires_at, ^generation}] ->
        :ets.delete_object(table, {key, expires_at, generation})

      _ ->
        false
    end
  end

  defp now_ms, do: System.monotonic_time(:millisecond)
end
