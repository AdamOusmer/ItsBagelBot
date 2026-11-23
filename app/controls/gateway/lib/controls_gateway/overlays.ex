# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays do
  @moduledoc "Queue pull loops, offer requester and their task supervisor."

  alias ControlsGateway.Overlays.{Delivery, Tiers}
  alias Platform.Nats.{Pull, Requester}

  @tasks ControlsGateway.Overlays.Tasks
  @offers ControlsGateway.Overlays.Offers
  @reply_prefix "overlays.session.reply"
  @inbox_prefix "_INBOX.overlays"

  def offers, do: @offers

  def children do
    [
      {Task.Supervisor, name: @tasks},
      {Requester, name: @offers, prefix: reply_prefix(), plane: :jetstream}
    ] ++ Enum.map(Tiers.all(), &puller/1)
  end

  def pause_pullers do
    for tier <- Tiers.all(), pid = Process.whereis(Tiers.puller(tier)), do: Pull.pause(pid)
    :ok
  end

  def await_idle do
    timeout_ms = Delivery.offer_timeout_ms()
    pullers = for tier <- Tiers.all(), pid = Process.whereis(Tiers.puller(tier)), do: pid

    pullers
    |> Task.async_stream(&Pull.await_idle(&1, timeout_ms),
      max_concurrency: max(length(pullers), 1),
      timeout: timeout_ms + 2_000
    )
    |> Stream.run()
  end

  def pullers_attached?, do: Enum.all?(Tiers.all(), &attached?/1)

  defp attached?(tier) do
    case Process.whereis(Tiers.puller(tier)) do
      nil -> false
      pid -> Pull.attached?(pid)
    end
  end

  defp reply_prefix,
    do: @reply_prefix <> "." <> Base.url_encode64(:crypto.strong_rand_bytes(12), padding: false)

  defp puller(tier) do
    {Pull,
     name: Tiers.puller(tier),
     stream: Tiers.stream(tier),
     durable: Tiers.durable(tier),
     handler: &Delivery.handle/1,
     task_supervisor: @tasks,
     inbox_prefix: @inbox_prefix,
     plane: :jetstream,
     expires_ns: Application.get_env(:controls_gateway, :pull_expires_ns, 25_000_000_000)}
  end
end
