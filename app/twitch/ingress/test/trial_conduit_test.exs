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

  test "new channels go to the coolest socket inside the target" do
    assert TrialConduit.slot_for_new([], 1) == 0
    assert TrialConduit.slot_for_new([row("a", 0, 50)], 1) == 0
    assert TrialConduit.slot_for_new([row("a", 0, 900), row("b", 1, 5)], 2) == 1
  end

  test "a hot socket first moves a channel into the coolest socket that has room" do
    rows = [row("hog", 0, 700), row("mid", 0, 350), row("b", 1, 200), row("c", 2, 900)]
    assert {%{broadcaster_id: "mid"}, 1} = move(rows, 3)
  end

  test "nothing moves when no existing socket has room, leaving the scale-up to the autoscaler" do
    rows = [row("a", 0, 600), row("b", 0, 500)]
    assert move(rows, 1) == nil
    assert {%{broadcaster_id: "b"}, 1} = move(rows, 2)
  end

  test "sockets under the budget are left alone" do
    assert move([], 1) == nil
    assert move([row("a", 0, 990), row("b", 0, 5), row("c", 1, 0)], 2) == nil
  end

  test "a lone channel over the budget stays because moving it only relocates it" do
    assert move([row("hog", 0, 5_000), row("b", 1, 10)], 2) == nil
  end

  test "scaling down drains the removed socket onto the coolest remaining one, even while settling" do
    rows = [row("a", 0, 300), row("b", 1, 10), %{row("c", 2, 50) | moved_at: @now - 5}]
    assert {%{broadcaster_id: "c"}, 1} = move(rows, 2)
  end

  test "no load move while a recent move is still settling" do
    rows = [row("a", 0, 600), row("b", 0, 500), %{row("c", 1) | moved_at: @now - 10}]
    assert move(rows, 2) == nil
    assert {_, 1} = TrialConduit.next_move(rows, 2, @now + 60, @budget)
  end

  test "disabled and stopping channels are never counted or moved" do
    off = %{row("a", 0, 900) | enabled: false}
    assert move([off, row("b", 0, 900, "stopping"), row("c", 0, 200)], 2) == nil
  end

  test "socket count follows the shard autoscaler policy" do
    budget = Policy.budget_per_window()
    ticks = Policy.reset_ticks()
    busy = [row("a", 0, budget), row("b", 0, div(budget, 10))]

    {1, ticks} = TrialConduit.scale(busy, 1, ticks)
    assert {2, _} = TrialConduit.scale(busy, 1, ticks)

    quiet = [row("a", 0, 10), row("b", 1, 10)]
    {2, ticks} = TrialConduit.scale(quiet, 2, Policy.reset_ticks())
    {2, ticks} = TrialConduit.scale(quiet, 2, ticks)
    assert {1, _} = TrialConduit.scale(quiet, 2, ticks)
    assert {1, _} = TrialConduit.scale([], 1, Policy.reset_ticks())

    overloaded = [row("a", 0, budget), row("b", 0, budget)]
    assert {2, _} = TrialConduit.scale(overloaded, 1, Policy.reset_ticks())
  end

  test "a trial budget override scales at that budget with the same policy" do
    rows = [row("a", 0, 800), row("b", 0, 700)]
    assert {1, _} = TrialConduit.scale(rows, 1, Policy.reset_ticks())
    assert {2, _} = TrialConduit.scale(rows, 1, Policy.reset_ticks(), @budget)
    assert {%{broadcaster_id: "b"}, 1} = move(rows, 2)
  end

  test "the budget defaults to the shard budget and follows TRIAL_SOCKET_BUDGET_EPS" do
    assert TrialConduit.budget() == Policy.budget_per_window()
    Application.put_env(:ingress, :trial_socket_budget_eps, 2)
    on_exit(fn -> Application.delete_env(:ingress, :trial_socket_budget_eps) end)
    assert TrialConduit.budget() == 120
  end
end
