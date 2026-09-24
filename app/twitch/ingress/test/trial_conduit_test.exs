defmodule Ingress.TrialConduitTest do
  use ExUnit.Case, async: false

  alias Ingress.ShardScaler.Policy
  alias Ingress.TrialConduit

  @now 10_000
  @budget 1_000

  defp row(id, slot, load \\ 0, state \\ "receiving"),
    do: %{
      broadcaster_id: id,
      slot: slot,
      load: load,
      received: 0,
      moved_at: 0,
      state: state,
      enabled: true
    }

  defp move(rows, target), do: TrialConduit.next_move(rows, target, @now, @budget)

  test "new channels go round robin to the socket with the fewest channels" do
    assert TrialConduit.slot_for_new([], 1) == 0
    assert TrialConduit.slot_for_new([row("a", 0, 50)], 1) == 1
    assert TrialConduit.slot_for_new([row("a", 0), row("b", 1)], 1) == 2
    assert TrialConduit.slot_for_new([row("a", 0), row("b", 1), row("c", 2)], 1) == 0

    assert TrialConduit.slot_for_new(
             [row("a", 0, 900), row("d", 0), row("b", 1, 5), row("c", 2, 40)],
             3
           ) == 1
  end

  test "the socket count never drops below one socket per channel, up to three" do
    assert TrialConduit.effective_target([], 1) == 1
    assert TrialConduit.effective_target([row("a", 0), row("b", 0)], 1) == 2

    assert TrialConduit.effective_target([row("a", 0), row("b", 0), row("c", 0), row("d", 0)], 1) ==
             3
  end

  test "channels piled on one socket are spread round robin, quietest first" do
    rows = [row("a", 0, 300), row("b", 0, 5), row("c", 0, 40)]
    assert {%{broadcaster_id: "b"}, 1} = move(rows, 1)
  end

  test "a spread that would push the emptier socket over the budget does not happen" do
    rows = [
      row("hog", 1, 995),
      row("a", 0, 10),
      row("b", 0, 10),
      row("c", 0, 10),
      row("d", 2, 10)
    ]

    assert move(rows, 3) == nil
  end

  test "a hot socket first moves the channel that best evens load into the coolest socket with room" do
    rows = [row("hog", 0, 700), row("mid", 0, 350), row("b", 1, 200), row("c", 2, 900)]
    assert {%{broadcaster_id: "mid"}, 1} = move(rows, 3)
  end

  test "nothing moves when no socket has room and counts are even" do
    rows = [row("a", 0, 900), row("b", 0, 600), row("c", 1, 900), row("d", 2, 900)]
    assert move(rows, 3) == nil
  end

  test "a lone channel over the budget stays because moving it only relocates it" do
    assert move([row("hog", 0, 5_000), row("b", 1, 10)], 2) == nil
  end

  test "channels on a socket above the target drain to the coolest remaining one, even while settling" do
    rows = [row("a", 0, 300), %{row("c", 2, 50) | moved_at: @now - 5}]
    assert {%{broadcaster_id: "c"}, 1} = move(rows, 1)
  end

  test "no load or spread move while a recent move is still settling" do
    rows = [row("a", 0, 600), row("b", 0, 500), %{row("c", 1) | moved_at: @now - 10}]
    assert move(rows, 2) == nil
    assert {%{broadcaster_id: "b"}, 2} = TrialConduit.next_move(rows, 2, @now + 60, @budget)
  end

  test "disabled and stopping channels are never counted or moved" do
    off = %{row("a", 0, 900) | enabled: false}
    assert move([off, row("b", 0, 900, "stopping"), row("c", 0, 200)], 2) == nil
  end

  test "the autoscaler adds a socket above the floor and scales back only to the floor" do
    budget = Policy.budget_per_window()
    busy = [row("a", 0, budget), row("b", 1, div(budget * 3, 2))]

    {2, ticks} = TrialConduit.scale(busy, 1, Policy.reset_ticks())
    assert {3, _} = TrialConduit.scale(busy, 2, ticks)

    quiet = [row("a", 0, 10), row("b", 1, 10)]
    {3, ticks} = TrialConduit.scale(quiet, 3, Policy.reset_ticks())
    {3, ticks} = TrialConduit.scale(quiet, 3, ticks)
    assert {2, _} = TrialConduit.scale(quiet, 3, ticks)
    assert {1, _} = TrialConduit.scale([], 1, Policy.reset_ticks())
  end

  test "a trial budget override scales at that budget with the same policy" do
    rows = [row("a", 0, 1_400), row("b", 1, 1_400)]
    assert {2, _} = TrialConduit.scale(rows, 2, Policy.reset_ticks())
    assert {3, _} = TrialConduit.scale(rows, 2, Policy.reset_ticks(), @budget)
  end

  test "the budget defaults to the shard budget and follows TRIAL_SOCKET_BUDGET_EPS" do
    assert TrialConduit.budget() == Policy.budget_per_window()
    Application.put_env(:ingress, :trial_socket_budget_eps, 2)
    on_exit(fn -> Application.delete_env(:ingress, :trial_socket_budget_eps) end)
    assert TrialConduit.budget() == 120
  end

  describe "yield_slot?/4" do
    defp owner(node, n), do: "#{n}:{#{inspect(node)}, #PID<0.1.0>}"

    test "a pod holding two sockets hands its highest one to a pod holding none" do
      a = :"ingress@10.0.0.1"
      b = :"ingress@10.0.0.2"
      owners = [owner(a, 1), owner(a, 2), nil]
      assert TrialConduit.yield_slot?(owners, 1, a, [a, b])
      refute TrialConduit.yield_slot?(owners, 0, a, [a, b])
    end

    test "no hand-off when every pod already holds one or there is nowhere to go" do
      a = :"ingress@10.0.0.1"
      b = :"ingress@10.0.0.2"
      c = :"ingress@10.0.0.3"
      refute TrialConduit.yield_slot?([owner(a, 1), owner(b, 2), owner(c, 3)], 0, a, [a, b, c])
      refute TrialConduit.yield_slot?([owner(a, 1), owner(a, 2), owner(b, 3)], 1, a, [a, b])
    end

    test "a node name that prefixes another is not mistaken for it" do
      a = :"ingress@10.0.0.2"
      b = :"ingress@10.0.0.23"
      refute TrialConduit.yield_slot?([owner(b, 1), owner(b, 2), nil], 1, a, [a, b])
    end
  end
end
