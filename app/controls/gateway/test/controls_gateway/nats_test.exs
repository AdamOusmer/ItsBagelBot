# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.NatsTest do
  use ExUnit.Case, async: false

  alias ControlsGateway.Nats
  alias ControlsGateway.TestSupport.FakeGnat

  @device_id "11111111-1111-4111-8111-111111111111"

  defp start_fake(id, opts) do
    start_supervised!(Supervisor.child_spec({FakeGnat, Keyword.put(opts, :name, :gnat)}, id: id))
  end

  test "a failed later sub unsubscribes the earlier sids and reports not_connected" do
    fake = start_fake(:fake, fail_sub_at: 2)

    assert Nats.subscribe_device(@device_id) == {:error, :not_connected}
    assert [{:sub, _}, {:sub, _}, {:unsub, 1}] = FakeGnat.calls(fake)
  end

  test "a successful subscribe makes no unsub calls" do
    fake = start_fake(:fake, [])

    assert {:ok, %{gnat: ^fake, sids: %{cmd: 1, takeover: 2, revoked: 3}}} =
             Nats.subscribe_device(@device_id)

    assert Enum.all?(FakeGnat.calls(fake), &match?({:sub, _}, &1))
  end

  test "unsubscribe only touches the connection the sids belong to" do
    start_fake(:old, [])
    {:ok, subs} = Nats.subscribe_device(@device_id)
    stop_supervised!(:old)
    replacement = start_fake(:new, [])

    assert Nats.unsubscribe_device(subs) == :ok
    assert FakeGnat.calls(replacement) == []
  end

  test "unsubscribe removes the sids on the same connection" do
    fake = start_fake(:fake, [])
    {:ok, subs} = Nats.subscribe_device(@device_id)

    assert Nats.unsubscribe_device(subs) == :ok

    assert fake |> FakeGnat.calls() |> Enum.drop(3) |> Enum.sort() == [
             unsub: 1,
             unsub: 2,
             unsub: 3
           ]
  end
end
