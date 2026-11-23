# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.LinkWatch do
  @moduledoc "Tells open streams to re-check their link when the overlays feed names their user."
  use GenServer

  alias Platform.Invalidations

  @streams ControlsGateway.Streams

  def start_link(feed), do: GenServer.start_link(__MODULE__, feed)

  @impl true
  def init(feed) do
    :ok = Invalidations.watch(feed, "overlays", &notify/1, __MODULE__)
    :ok = Invalidations.watch_gap(feed, fn -> notify(%{id: :all}) end, __MODULE__)
    {:ok, feed}
  end

  defp notify(event) do
    Enum.each(pids(event), &notify_later/1)
  end

  defp notify_later(pid) do
    Process.send_after(pid, :link_changed, Platform.Backoff.between_ms(100, 1_000))
  end

  defp pids(%{id: :all}), do: Registry.select(@streams, [{{:_, :"$1", :_}, [], [:"$1"]}])
  defp pids(%{id: user_id}), do: for({pid, _value} <- Registry.lookup(@streams, user_id), do: pid)
end
