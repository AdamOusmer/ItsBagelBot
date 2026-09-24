# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Squash do
  use GenServer

  alias Ingress.Config.Squash, as: SquashConfig
  alias Ingress.{Config, Metrics, Nats, Pipeline}

  @keys_table __MODULE__.Keys

  @type base :: %{
          broadcaster_user_id: String.t(),
          broadcaster_user_login: String.t() | nil,
          lane: :premium | :standard,
          text: String.t(),
          emotes: [map()]
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

  @spec observe(base(), sender(), GenServer.server()) :: :first | :buffered
  def observe(base, sender, server \\ __MODULE__) do
    observe_keyed(server, &prepared_key/1, base, {:prepared, base, sender})
  end

  @spec observe_chat(:premium | :standard, map(), String.t(), map()) :: :first | :buffered
  def observe_chat(lane, event, text, meta) do
    observe_keyed(__MODULE__, &chat_key/1, {event, text, meta}, {:chat, lane, event, text, meta})
  end

  defp prepared_key(base) do
    {base.broadcaster_user_id, base[:origin], base[:trial_generation], String.trim(base.text)}
  end

  defp chat_key({event, text, meta}) do
    {event["broadcaster_user_id"], Map.get(meta, :origin), Map.get(meta, :trial_generation),
     String.trim(text)}
  end

  defp observe_keyed(server, key_of, source, entry) do
    key = key_of.(source)

    with %{table: table, server: owner, window_ms: window_ms} <- context(server, key) do
      do_observe(table, owner, key, entry, window_ms)
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
    window_ms = Keyword.get(opts, :window_ms) || SquashConfig.window_ms()

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
      max_senders: Keyword.get(opts, :max_senders) || SquashConfig.max_senders(),
      sweep_ms: Keyword.get(opts, :sweep_ms) || SquashConfig.sweep_ms(),
      publish: Keyword.get(opts, :publish, &__MODULE__.publish_cohort/2)
    }

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:ok, state}
  end

  @impl true
  def handle_cast({:dup, key, generation, base, sender}, state) do
    cohort_key = {key, generation}

    if current?(state.table, key, :any, generation) do
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
            {nil, cohorts} ->
              %{state | cohorts: cohorts}

            {cohort, cohorts} ->
              emit(cohort, state)
              %{state | cohorts: cohorts}
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

    expired =
      :ets.select(state.table, [
        {{:"$1", :"$2", :"$3"}, [{:"=<", :"$2", now}], [{{:"$1", :"$2", :"$3"}}]}
      ])

    cohorts =
      Enum.reduce(expired, state.cohorts, fn {key, expires_at, generation}, acc ->
        if current?(state.table, key, expires_at, generation) do
          :ets.delete(state.table, key)

          case Map.pop(acc, {key, generation}) do
            {nil, acc} ->
              acc

            {cohort, acc} ->
              emit(cohort, state)
              acc
          end
        else
          acc
        end
      end)

    Process.send_after(self(), :sweep, state.sweep_ms)
    {:noreply, %{state | cohorts: cohorts}}
  end

  defp emit(%{base: base, senders: senders, count: count}, state) do
    distinct = senders |> Enum.map(& &1.chatter_user_id) |> Enum.uniq() |> length()
    ordered = Enum.reverse(senders)

    message = %{
      type: "channel.chat.message",
      lane: base.lane,
      broadcaster_user_id: base.broadcaster_user_id,
      broadcaster_user_login: base.broadcaster_user_login,
      origin: base[:origin],
      trial_generation: base[:trial_generation],
      text: base.text,
      msg_id: List.first(ordered).msg_id,
      senders: ordered,
      count: count,
      distinct_users: distinct
    }

    message =
      case base[:emotes] do
        [_ | _] = emotes -> Map.put(message, :emotes, emotes)
        _ -> message
      end

    Metrics.count("Cohorts/Emitted")
    Metrics.count("Cohorts/Senders", count)
    state.publish.(Config.hot_lane_subject(base.lane), message)
    true
  end

  @doc false
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
      origin: if(Map.get(meta, :origin) == :trial, do: "trial"),
      trial_generation: Map.get(meta, :trial_generation),
      lane: lane,
      text: text,
      emotes: Pipeline.emote_spans(event)
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

  defp current?(table, key, expires_at, generation) do
    case :ets.lookup(table, key) do
      [{^key, stored, ^generation}] -> expires_at == :any or stored == expires_at
      _stale_or_absent -> false
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
