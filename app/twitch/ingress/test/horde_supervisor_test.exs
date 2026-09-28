# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.HordeSupervisorTest do
  use ExUnit.Case, async: false

  alias Ingress.{ConduitManager, HordeSupervisor, ShardSession}

  defmodule Child do
    use GenServer
    def start_link(opts), do: GenServer.start_link(__MODULE__, [], Keyword.take(opts, [:name]))
    def init(_), do: {:ok, nil}
    def handle_call(:status, _, state), do: {:reply, %{bound: true, node: node()}, state}
    def handle_cast(_, state), do: {:noreply, state}
  end

  defmodule LostReply do
    use GenServer
    def start_link(parent), do: GenServer.start_link(__MODULE__, parent)
    def init(parent), do: {:ok, parent}

    def handle_call({:start_child, spec}, from, parent) do
      send(parent, {:pending_start, self(), from, spec})
      {:noreply, parent}
    end

    def handle_call({:reply_with, reply}, _, parent), do: {:reply, reply, parent}
    def handle_call(:which_children, _, parent), do: {:reply, [], parent}
    def handle_call({:terminate_child, _}, _, parent), do: {:noreply, parent}

    def handle_info({:late_reply, from, reply}, parent) do
      GenServer.reply(from, reply)
      {:noreply, parent}
    end
  end

  defmodule ExpiredTermination do
    use GenServer
    def start_link(_), do: GenServer.start_link(__MODULE__, nil)
    def init(_), do: {:ok, nil}

    def handle_call({:terminate_child, _} = message, _, state),
      do: {:reply, {:error, :proxy_operation_ttl_expired, message}, state}
  end

  defmodule DelayedStart do
    use GenServer
    def start_link(opts), do: GenServer.start_link(__MODULE__, opts)
    def init(opts), do: {:ok, Map.new(opts)}

    def handle_call({:start_child, spec}, from, %{delay: delay} = state) do
      Task.start(fn ->
        Process.sleep(delay)
        result = Horde.DynamicSupervisor.start_child(state.supervisor, spec)
        GenServer.reply(from, result)
        send(state.parent, {:late_start, result})
      end)

      {:noreply, state}
    end
  end

  defmodule DisagreeingDistribution do
    @behaviour Horde.DistributionStrategy
    def choose_node(_, members) do
      case Enum.find(members, &(&1.status == :alive)) do
        nil -> {:error, :no_alive_nodes}
        member -> {:ok, member}
      end
    end

    def has_quorum?(_), do: true
  end

  test "lost start replies time out without spawning callers or poisoning their mailbox" do
    server = start_supervised!({LostReply, self()})
    assert {:error, :timeout} = HordeSupervisor.start_child(server, {Child, []}, 20)
    assert_receive {:pending_start, ^server, {caller, tag} = from, spec}
    assert caller == self()
    assert %{start: {Child, :start_link, [[]]}} = spec
    send(server, {:late_reply, from, {:ok, self()}})
    assert [] = HordeSupervisor.which_children(server, 100)
    refute_receive {^tag, _}, 20
  end

  test "lost termination replies are bounded and dead supervisors return errors" do
    server = start_supervised!({LostReply, self()})
    assert {:error, :timeout} = HordeSupervisor.terminate_child(server, self(), 20)
    monitor = Process.monitor(server)
    GenServer.stop(server)
    assert_receive {:DOWN, ^monitor, :process, ^server, _}
    assert {:error, {:supervisor_unavailable, _}} = HordeSupervisor.which_children(server, 20)
  end

  test "expired forwarding errors preserve the usual two-element error contract" do
    server = start_supervised!({ExpiredTermination, []})
    pid = self()

    assert {:error, {:proxy_operation_ttl_expired, {:terminate_child, ^pid}}} =
             HordeSupervisor.terminate_child(server, pid)
  end

  test "finite application forwarding budget completes a start under conflicting member views" do
    options = Ingress.Application.shard_supervisor_options()
    assert options[:proxy_message_ttl] == 5
    assert options[:process_redistribution] == :passive
    first = unique_name("First")
    second = unique_name("Second")
    members = [{first, node()}, {second, node()}]
    start_horde(first, members: members, distribution_strategy: DisagreeingDistribution)
    start_horde(second, members: members, distribution_strategy: DisagreeingDistribution)
    await_members([first, second])

    for {own, other} <- [{first, second}, {second, first}] do
      :sys.replace_state(own, fn state ->
        own_member = %Horde.DynamicSupervisor.Member{name: {own, node()}, status: :uninitialized}
        other_member = %Horde.DynamicSupervisor.Member{name: {other, node()}, status: :alive}
        %{state | members_info: %{{own, node()} => own_member, {other, node()} => other_member}}
      end)
    end

    assert {:ok, pid} = HordeSupervisor.start_child(first, {Child, []}, 1_000)
    assert Process.alive?(pid)
  end

  test "late rescue success is deduplicated and recovered even without another healing attempt" do
    start_registry()
    supervisor = unique_name("Rescue")
    start_horde(supervisor)
    proxy = start_supervised!({DelayedStart, supervisor: supervisor, parent: self(), delay: 50})
    spec = {Child, name: ShardSession.rescue_via(7)}

    assert {:error, :timeout} = HordeSupervisor.start_child(proxy, spec, 10)
    assert_receive {:late_start, {:ok, pid}}, 1_000
    assert Process.alive?(pid)
    assert [{^pid, _}] = Horde.Registry.lookup(Ingress.Registry, {:rescue, 7})
    assert {:error, {:already_started, ^pid}} = HordeSupervisor.start_child(supervisor, spec)

    state = ConduitManager.refresh_rescues(%{rescues: %{}, unhealthy_counts: %{7 => 2}})
    assert state.rescues == %{7 => {pid, 2}}
    refreshed = ConduitManager.refresh_rescues(%{state | unhealthy_counts: %{7 => 8}})
    assert refreshed.rescues == state.rescues
    assert [{_, ^pid, _, _}] = HordeSupervisor.which_children(supervisor)
    assert :ok = HordeSupervisor.terminate_child(supervisor, pid)
    refute Process.alive?(pid)
  end

  test "rescue conflicts stop the losing socket and takeover reclaims only its rescue key" do
    start_registry()
    winner = start_supervised!({Child, []})
    state = %ShardSession{shard_id: 4, name_state: :rescue}
    conflict = {:EXIT, winner, {:name_conflict, {{:rescue, 4}, nil}, Ingress.Registry, winner}}
    assert {:stop, :normal, stopped} = ShardSession.handle_info(conflict, state)
    assert stopped.primary == nil

    ref = make_ref()
    state = %{state | takeover: %{monitor: ref, timer: nil}}

    assert {:noreply, recovered} =
             ShardSession.handle_info({:DOWN, ref, :process, winner, :normal}, state)

    assert recovered.name_state == :rescue
    assert [{owner, _}] = Horde.Registry.lookup(Ingress.Registry, {:rescue, 4})
    assert owner == self()
    assert [] = Horde.Registry.lookup(Ingress.Registry, {:shard, 4})

    assert {:reply, {:error, :rescue_session}, ^recovered} =
             ShardSession.handle_call(:release_name, self(), recovered)

    assert {:reply, {:error, :rescue_session}, ^recovered} =
             ShardSession.handle_call(:reclaim_name, self(), recovered)
  end

  defp start_registry do
    start_supervised!({Horde.Registry, name: Ingress.Registry, keys: :unique, members: :auto})
  end

  defp start_horde(name, extra \\ []) do
    options =
      Ingress.Application.shard_supervisor_options()
      |> Keyword.merge(name: name, distribution_strategy: Horde.UniformDistribution)
      |> Keyword.merge(extra)

    start_supervised!({Horde.DynamicSupervisor, options})
  end

  defp unique_name(label),
    do: Module.concat(__MODULE__, label <> Integer.to_string(System.unique_integer([:positive])))

  defp await_members(supervisors),
    do: await_members(supervisors, System.monotonic_time(:millisecond) + 10_000)

  defp await_members(supervisors, deadline_ms) do
    views = Enum.map(supervisors, &:sys.get_state(&1).members_info)

    cond do
      Enum.all?(views, &all_alive?/1) ->
        :ok

      System.monotonic_time(:millisecond) > deadline_ms ->
        flunk("Horde member metadata failed to converge: #{inspect(views)}")

      true ->
        Process.sleep(10)
        await_members(supervisors, deadline_ms)
    end
  end

  defp all_alive?(members_info),
    do:
      map_size(members_info) == 2 and
        Enum.all?(members_info, fn {_, member} -> member.status == :alive end)
end
