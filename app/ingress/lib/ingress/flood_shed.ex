defmodule Ingress.FloodShed do
  @moduledoc """
  Per-channel rate cap for plain chat, the load guard that replaces the old `!`
  filter's implicit ceiling.

  With `!` lifted, every chat line flows. `Ingress.Squash` collapses identical
  lines (emote spam, copypasta, unsalted raids), so those never reach here. What
  remains is DISTINCT plain chat: a hate raid that salts each message, or a
  genuine flood. This caps that at `flood_shed_per_sec` messages per channel per
  second; anything over is shed, which still leaves the worker dozens of distinct
  senders a second, more than enough for the automod to see the raid and drive
  Shield Mode, while NATS and the worker never drown.

  Commands and special users never reach here (they are published directly), so
  a legitimate `!followage` is never shed and the cooldown gates command abuse.

  A fixed per-second window keyed by `{broadcaster_id, unix_second}` is
  incremented with an atomic, lock-free `:ets.update_counter` from the calling
  process; the GenServer only owns the table and sweeps stale buckets. Per-pod
  state is sufficient: a channel's chat is owned by one shard on one node.
  """

  use GenServer

  alias Ingress.Config

  @table __MODULE__.Buckets

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @doc """
  True when the channel is under its per-second budget (publish); false when it
  is over (shed). Fails open (`true`) when the table is absent so a missing
  guard never drops traffic.
  """
  @spec allow?(String.t()) :: boolean()
  def allow?(broadcaster_id) do
    key = {broadcaster_id, System.system_time(:second)}
    :ets.update_counter(@table, key, {2, 1}, {key, 0}) <= limit()
  rescue
    ArgumentError -> true
  end

  @impl true
  def init(opts) do
    :ets.new(@table, [
      :set,
      :public,
      :named_table,
      write_concurrency: true,
      read_concurrency: true
    ])

    :persistent_term.put(
      {__MODULE__, :limit},
      Keyword.get(opts, :per_sec) || Config.flood_shed_per_sec()
    )

    sweep_ms = Keyword.get(opts, :sweep_ms) || Config.flood_shed_sweep_ms()
    Process.send_after(self(), :sweep, sweep_ms)
    {:ok, %{sweep_ms: sweep_ms}}
  end

  @impl true
  def handle_info(:sweep, state) do
    # Keep the current and previous second; drop everything older.
    cutoff = System.system_time(:second) - 1
    :ets.select_delete(@table, [{{{:_, :"$1"}, :_}, [{:<, :"$1", cutoff}], [true]}])
    Process.send_after(self(), :sweep, state.sweep_ms)
    {:noreply, state}
  end

  defp limit, do: :persistent_term.get({__MODULE__, :limit}, 40)
end
