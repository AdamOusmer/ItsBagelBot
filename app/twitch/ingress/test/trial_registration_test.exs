# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TrialRegistrationFakeGnat do
  use GenServer

  def start_link(test), do: GenServer.start_link(__MODULE__, test, name: :gnat)

  @impl true
  def init(test), do: {:ok, %{test: test, sid: 0, registered: MapSet.new()}}

  @impl true
  def handle_call({:register, id}, _from, state),
    do: {:reply, :ok, %{state | registered: MapSet.put(state.registered, id)}}

  def handle_call({:request, %{topic: topic, body: body, recipient: recipient}}, _from, state) do
    sid = state.sid + 1
    send(state.test, {:request, topic})
    send(recipient, {:msg, %{topic: sid, body: reply(topic, body, state)}})
    {:reply, {:ok, sid}, %{state | sid: sid}}
  end

  def handle_call({:unsub, _sid, _opts}, _from, state), do: {:reply, :ok, state}

  defp reply("bagel.rpc.internal.users.get" <> _, body, state) do
    %{"user_id" => id} = Jason.decode!(body)

    if MapSet.member?(state.registered, id),
      do: Jason.encode!(%{user: %{id: id}}),
      else: Jason.encode!(%{code: "not_found"})
  end

  defp reply(_topic, _body, _state), do: Jason.encode!(%{deleted: true})
end

defmodule Ingress.TrialRegistrationFakeWs do
  def connect(_url), do: {:error, :offline}
  def close(_socket), do: :ok
  def stream(_socket, _message), do: :unknown
end

defmodule Ingress.TrialRegistrationTest do
  use ExUnit.Case, async: false

  if is_nil(System.find_executable("valkey-server")),
    do: @moduletag(skip: "valkey-server is not installed")

  alias Ingress.{
    EnvCase,
    TrialMembership,
    TrialReceiver,
    TrialRegistrationFakeGnat,
    TrialRegistrationFakeWs,
    TrialUserChanged,
    Trials
  }

  setup do
    if System.find_executable("valkey-server") do
      fixture = Ingress.TrialValkeyFixture.start()
      start_supervised!({Ingress.TrialValkey, []})
      assert :ok == Ingress.TrialValkeyFixture.await_ready()
      on_exit(fn -> Ingress.TrialValkeyFixture.stop(fixture) end)
      EnvCase.put_env(eventsub_url: "ws://127.0.0.1:1")
      start_supervised!({TrialRegistrationFakeGnat, self()})
    end

    :ok
  end

  defp start_receiver do
    start_supervised!({TrialReceiver, slot: 0, ws_module: TrialRegistrationFakeWs})
  end

  defp add_on_slot_zero(id) do
    {:ok, generation} = Trials.add(id)
    Trials.move(id, generation, 0)
    assert row(id).slot == 0
  end

  defp row(id) do
    {:ok, %{trials: rows}} = Trials.list()
    Enum.find(rows, &(&1.broadcaster_id == id))
  end

  defp flush_requests(acc \\ []) do
    receive do
      {:request, topic} -> flush_requests([topic | acc])
    after
      0 -> acc
    end
  end

  defp user_lookups(topics),
    do: Enum.count(topics, &String.starts_with?(&1, "bagel.rpc.internal.users.get"))

  test "ticks never ask the users service about registration" do
    add_on_slot_zero("42")
    add_on_slot_zero("43")
    pid = start_receiver()
    assert EnvCase.eventually(fn -> TrialReceiver.status(pid).owned end)

    for _ <- 1..5 do
      send(pid, :tick)
      TrialReceiver.status(pid)
    end

    lookups = user_lookups(flush_requests())
    assert lookups == 2
    assert row("42").state != "promoted"
  end

  test "ownership takeover promotes an owned trial that registered while no pod listened" do
    add_on_slot_zero("42")
    add_on_slot_zero("43")
    :ok = GenServer.call(:gnat, {:register, "42"})
    pid = start_receiver()

    assert EnvCase.eventually(fn ->
             send(pid, :tick)
             row("42").state == "promoted"
           end)

    assert row("43").state != "promoted"
  end

  test "registration event promotes an owned trial row without any users lookup" do
    add_on_slot_zero("42")
    start_supervised!(TrialMembership)
    assert EnvCase.eventually(fn -> TrialMembership.lookup("42") end)
    pid = start_receiver()
    assert EnvCase.eventually(fn -> TrialReceiver.status(pid).owned end)
    flush_requests()

    assert :ok == TrialUserChanged.request(%{body: Jason.encode!(%{user_id: 42})})

    assert row("42").stop_reason == "promoted"

    assert EnvCase.eventually(fn ->
             send(pid, :tick)
             row("42").state == "promoted"
           end)

    assert user_lookups(flush_requests()) == 0
  end

  test "registration event for an unknown broadcaster changes nothing" do
    add_on_slot_zero("42")
    start_supervised!(TrialMembership)
    assert EnvCase.eventually(fn -> TrialMembership.lookup("42") end)

    assert :ok == TrialUserChanged.request(%{body: Jason.encode!(%{user_id: 777})})

    assert row("777") == nil
    assert row("42").state == "pending"
    assert flush_requests() == []
  end
end
