# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ClusterDiscoveryTest do
  use ExUnit.Case, async: false

  @a :"ingress@10.0.0.1"
  @b :"ingress@10.0.0.2"
  @service ~c"ingress-test"

  setup do
    previous = System.get_env("BAGELBOT_K8S_HEADLESS_SERVICE")
    System.put_env("BAGELBOT_K8S_HEADLESS_SERVICE", to_string(@service))

    on_exit(fn ->
      if previous,
        do: System.put_env("BAGELBOT_K8S_HEADLESS_SERVICE", previous),
        else: System.delete_env("BAGELBOT_K8S_HEADLESS_SERVICE")
    end)

    :ok
  end

  test "runtime DNS topology retains established peers through a partial answer" do
    {:ok, network} =
      Agent.start_link(fn ->
        %{addresses: [{10, 0, 0, 1}, {10, 0, 0, 2}], alive: [@a, @b], connected: []}
      end)

    topology = runtime_topology()
    assert topology[:disconnect] == {Ingress.ClusterDiscovery, :retain_connection, []}

    topology =
      topology
      |> Keyword.put(:connect, {__MODULE__, :connect, [network]})
      |> Keyword.put(:list_nodes, {__MODULE__, :connected, [network]})
      |> Keyword.update!(:config, fn config ->
        config
        |> Keyword.put(:polling_interval, 60_000)
        |> Keyword.put(:resolver, fn _ ->
          addresses = Agent.get(network, & &1.addresses)
          {:ok, {:hostent, @service, [], :inet, 4, addresses}}
        end)
      end)

    parent = self()
    handler = {__MODULE__, make_ref()}

    :ok =
      :telemetry.attach(
        handler,
        [:libcluster, :disconnect_node, :ok],
        &__MODULE__.observe_removal/4,
        parent
      )

    on_exit(fn -> :telemetry.detach(handler) end)

    supervisor = start_supervised!({Cluster.Supervisor, [[ingress: topology]]})
    [{_, strategy, _, _}] = Supervisor.which_children(supervisor)
    assert :sys.get_state(strategy).meta == MapSet.new([@a, @b])
    assert Enum.sort(connected(network)) == [@a, @b]

    Agent.update(network, &%{&1 | addresses: [{10, 0, 0, 1}]})
    send(strategy, :load)
    assert :sys.get_state(strategy).meta == MapSet.new([@a])
    assert_receive {:removal_acknowledged, @b}
    assert Enum.sort(connected(network)) == [@a, @b]

    Agent.update(network, &%{&1 | addresses: [{10, 0, 0, 1}, {10, 0, 0, 2}]})
    send(strategy, :load)
    assert :sys.get_state(strategy).meta == MapSet.new([@a, @b])
    assert Enum.sort(connected(network)) == [@a, @b]

    # Actual node loss remains authoritative. An absent DNS peer is not
    # reconnected, retained in the discovered set, or passed to the callback.
    Agent.update(network, &%{&1 | addresses: [{10, 0, 0, 1}], alive: [@a], connected: [@a]})
    send(strategy, :load)
    assert :sys.get_state(strategy).meta == MapSet.new([@a])
    assert connected(network) == [@a]
    refute_receive {:removal_acknowledged, @b}
  end

  test "acknowledging an already absent peer does not connect it or raise" do
    peer = :"ingress@192.0.2.250"
    refute peer in Node.list()
    assert Ingress.ClusterDiscovery.retain_connection(peer)
    refute peer in Node.list()
  end

  def observe_removal(_, _, metadata, parent),
    do: send(parent, {:removal_acknowledged, metadata.node})

  def connect(network, node) do
    Agent.get_and_update(network, fn state ->
      if node in state.alive do
        {true, %{state | connected: Enum.uniq([node | state.connected])}}
      else
        {false, state}
      end
    end)
  end

  def connected(network), do: Agent.get(network, & &1.connected)

  defp runtime_topology do
    {config, _warnings} =
      ExUnit.CaptureIO.with_io(:stderr, fn ->
        Config.Reader.read!("config/runtime.exs", env: :test)
      end)

    config[:ingress][:cluster_topologies][:ingress]
  end
end
