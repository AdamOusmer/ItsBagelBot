defmodule Ingress.TrialValkeyFixture do
  alias Ingress.TrialValkey

  def start do
    dir = Path.join(System.tmp_dir!(), "bagel-trial-#{System.unique_integer([:positive])}")
    File.mkdir_p!(dir)
    port = free_port()
    previous = System.get_env("VALKEY_ADDR")
    System.put_env("VALKEY_ADDR", "127.0.0.1:#{port}")

    {_output, 0} =
      System.cmd("valkey-server", [
        "--bind",
        "127.0.0.1",
        "--port",
        Integer.to_string(port),
        "--save",
        "",
        "--appendonly",
        "no",
        "--daemonize",
        "yes",
        "--pidfile",
        Path.join(dir, "valkey.pid")
      ])

    {dir, previous}
  end

  def await_ready do
    Enum.reduce_while(1..50, :unavailable, fn _, _ ->
      case TrialValkey.command(["PING"]) do
        {:ok, "PONG"} ->
          {:halt, :ok}

        _ ->
          Process.sleep(20)
          {:cont, :unavailable}
      end
    end)
  end

  def stop({dir, previous}) do
    _ = TrialValkey.command(["SHUTDOWN", "NOSAVE"])

    if previous,
      do: System.put_env("VALKEY_ADDR", previous),
      else: System.delete_env("VALKEY_ADDR")

    File.rm_rf!(dir)
  end

  defp free_port do
    {:ok, listener} = :gen_tcp.listen(0, [:binary, active: false, ip: {127, 0, 0, 1}])
    {:ok, {_address, port}} = :inet.sockname(listener)
    :gen_tcp.close(listener)
    port
  end
end

defmodule Ingress.TrialsTest do
  use ExUnit.Case, async: false

  if is_nil(System.find_executable("valkey-server")),
    do: @moduletag(skip: "valkey-server is not installed")

  alias Ingress.{TrialValkey, Trials}

  setup do
    if System.find_executable("valkey-server") do
      fixture = Ingress.TrialValkeyFixture.start()
      start_supervised!({TrialValkey, []})
      assert :ok == Ingress.TrialValkeyFixture.await_ready()
      on_exit(fn -> Ingress.TrialValkeyFixture.stop(fixture) end)
    end

    :ok
  end

  test "concurrent admissions reserve no more than thirty channels and converge on duplicates" do
    outcomes =
      1..40
      |> Task.async_stream(fn id -> Trials.add(Integer.to_string(id)) end,
        max_concurrency: 40,
        timeout: 5_000
      )
      |> Enum.map(fn {:ok, value} -> value end)

    assert Enum.count(outcomes, &match?({:ok, _}, &1)) == 30
    assert Enum.count(outcomes, &(&1 == {:error, "full"})) == 10
    {:ok, %{active_count: 30, max_channels: 30, socket_target: 1, trials: rows}} = Trials.list()
    assert length(rows) == 30
    assert Enum.all?(rows, &(&1.slot in 0..2))

    id = hd(rows).broadcaster_id
    {:ok, _} = Trials.field(id, "decoded", 3)
    {:ok, _} = Trials.field(id, "latency_samples", 2)
    {:ok, _} = Trials.field(id, "latency_total_ms", 25)
    generation = hd(rows).generation
    {:ok, 1} = Trials.display_name(id, generation, "Sample Streamer")
    {:ok, 0} = Trials.display_name(id, "stale", "Wrong Name")
    {:ok, %{trials: measured}} = Trials.list()

    assert Enum.any?(
             measured,
             &(&1.broadcaster_id == id and &1.decoded == 3 and
                 &1.average_processing_latency_ms == 12 and &1.display_name == "Sample Streamer")
           )

    {:ok, "duplicate"} = Trials.add(id)
    {:ok, "stopping"} = Trials.stop(id)
    {:ok, "stopping"} = Trials.stop(id)
    {:ok, 1} = Trials.finish(id, "removed")
    {:ok, %{active_count: 29, trials: history}} = Trials.list()
    assert Enum.any?(history, &(&1.broadcaster_id == id && &1.state == "removed"))
  end

  test "lease epoch fences a second owner per socket slot" do
    {:ok, epoch} = Trials.acquire("owner-one")
    assert {:error, :owned} = Trials.acquire("owner-two")
    {:ok, 1} = Trials.renew("owner-one", epoch)
    {:ok, 0} = Trials.renew("owner-two", epoch)

    {:ok, other} = Trials.acquire("owner-two", 2)
    {:ok, 0} = Trials.renew("owner-two", other, 1)
    {:ok, 1} = Trials.renew("owner-two", other, 2)
    assert {:ok, ["#{epoch}:owner-one", nil, "#{other}:owner-two"]} == Trials.owners()
  end

  test "move reassigns only live enabled channels of the current generation" do
    {:ok, generation} = Trials.add("42")
    {:ok, 0} = Trials.move("42", "stale", 2)
    {:ok, 1} = Trials.move("42", generation, 2)
    {:ok, %{trials: [%{slot: 2, moved_at: moved_at}]}} = Trials.list()
    assert moved_at > 0
    {:ok, 0} = Trials.record_load("42", generation, 0, 120)
    {:ok, 1} = Trials.record_load("42", generation, 2, 120)
    {:ok, %{trials: [%{load: 120}]}} = Trials.list()
    {:ok, "disabled"} = Trials.set_enabled("42", false)
    {:ok, 0} = Trials.move("42", generation, 1)
  end

  test "disabled rows retain their slot and counters while atomic admission rejects stale frames" do
    {:ok, generation} = Trials.add("42")
    {:ok, _} = Trials.field("42", "state", "receiving")
    assert :first == Trials.admit("42", generation, "message-1")
    assert :duplicate == Trials.admit("42", generation, "message-1")

    assert {:ok, "disabled"} == Trials.set_enabled("42", false)
    assert :inactive == Trials.admit("42", generation, "message-2")
    assert {:ok, "duplicate"} == Trials.add("42")
    assert {:ok, %{active_count: 1, trials: [row]}} = Trials.list()
    assert row.enabled == false
    assert row.state == "disabled"
    assert row.received == 1

    for id <- 43..71, do: assert({:ok, _} = Trials.add(Integer.to_string(id)))
    assert {:error, "full"} == Trials.add("72")

    assert {:ok, "pending"} == Trials.set_enabled("42", true)
    assert :inactive == Trials.admit("42", generation, "message-3")
    {:ok, _} = Trials.field("42", "state", "receiving")
    assert :first == Trials.admit("42", generation, "message-3")
    assert :inactive == Trials.admit("42", "stale-generation", "message-4")
    assert {:error, "not_found"} == Trials.set_enabled("72", false)
  end

  test "rapid re-enable drains the old subscription before permitting a new one" do
    {:ok, generation} = Trials.add("42")
    {:ok, _} = Trials.field("42", "state", "receiving")
    {:ok, _} = Trials.field("42", "subscription_id", "old-sub")
    assert {:ok, "disabled"} == Trials.set_enabled("42", false)
    assert {:ok, "disabled"} == Trials.set_enabled("42", true)
    assert :inactive == Trials.admit("42", generation, "message-1")

    assert {:ok, %{trials: [%{enabled: true, state: "disabled", subscription_id: "old-sub"}]}} =
             Trials.list()
  end
end

defmodule Ingress.TrialFakeWS do
  def connect(url) do
    id = make_ref()
    send(Application.fetch_env!(:ingress, :trial_test_pid), {:trial_connected, id, url})
    {:ok, %{id: id}}
  end

  def stream(nil, _), do: :unknown
  def stream(%{id: id} = socket, {:fake_ws, id, events}), do: {:ok, socket, events}
  def stream(_, _), do: :unknown
  def send_frame(socket, _), do: {:ok, socket}
  def close(nil), do: :ok

  def close(%{id: id}) do
    send(Application.fetch_env!(:ingress, :trial_test_pid), {:trial_closed, id})
    :ok
  end
end

defmodule Ingress.TrialReceiverProtocolTest do
  use ExUnit.Case, async: false

  if is_nil(System.find_executable("valkey-server")),
    do: @moduletag(skip: "valkey-server is not installed")

  alias Ingress.{JSON, TrialAdmission, TrialReceiver, TrialValkey, Trials}

  setup do
    if System.find_executable("valkey-server") do
      fixture = Ingress.TrialValkeyFixture.start()
      Application.put_env(:ingress, :trial_test_pid, self())
      start_supervised!({TrialValkey, []})
      start_supervised!(TrialAdmission.Pool)
      assert :ok == Ingress.TrialValkeyFixture.await_ready()

      on_exit(fn ->
        Ingress.TrialValkeyFixture.stop(fixture)
        Application.delete_env(:ingress, :trial_test_pid)
      end)
    end

    :ok
  end

  defp settle_admission do
    for p <- 0..(TrialAdmission.partitions() - 1), do: :sys.get_state(TrialAdmission.name(p))
  end

  defp frame(type, payload) do
    body =
      JSON.encode(%{metadata: %{message_type: type}, payload: payload}) |> IO.iodata_to_binary()

    {:frame, {:text, body}}
  end

  test "welcome, directed reconnect and revocation update receiver state" do
    {:ok, _} = Trials.add("4242")
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    assert_receive {:trial_connected, primary, _url}, 1_000

    send(
      pid,
      {:fake_ws, primary,
       [frame("session_welcome", %{session: %{id: "s1", keepalive_timeout_seconds: 30}})]}
    )

    assert :sys.get_state(pid).session_id == "s1"

    {:ok, _} = Trials.field("4242", "state", "receiving")
    {:ok, _} = Trials.field("4242", "session_id", "s1")
    {:ok, _} = Trials.field("4242", "subscription_id", "sub-1")

    send(
      pid,
      {:fake_ws, primary,
       [frame("session_reconnect", %{session: %{reconnect_url: "wss://replacement.invalid/ws"}})]}
    )

    assert_receive {:trial_connected, pending, "wss://replacement.invalid/ws"}, 1_000
    assert :sys.get_state(pid).pending.id == pending

    send(pid, {:fake_ws, pending, [frame("session_welcome", %{session: %{id: "s2"}})]})
    assert :sys.get_state(pid).socket.id == pending
    assert :sys.get_state(pid).session_id == "s2"
    {:ok, owner_session} = TrialValkey.command(["GET", "trial:owner_session"])
    assert String.ends_with?(owner_session, ":s2")
    assert_receive {:trial_closed, ^primary}, 1_000

    send(pid, :tick)
    :sys.get_state(pid)
    {:ok, %{trials: [%{state: "receiving", subscription_id: "sub-1"}]}} = Trials.list()

    send(
      pid,
      {:fake_ws, pending,
       [frame("revocation", %{subscription: %{condition: %{broadcaster_user_id: "4242"}}})]}
    )

    :sys.get_state(pid)
    {:ok, %{trials: [%{state: "failed", error: "subscription_revoked"}]}} = Trials.list()
  end

  test "socket opens with the first desired trial and closes after the last finishes" do
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    refute_receive {:trial_connected, _, _}, 100
    {:ok, _} = Trials.add("4242")
    send(pid, :tick)
    assert_receive {:trial_connected, primary, _}, 1_000
    {:ok, "stopping"} = Trials.stop("4242")
    {:ok, 1} = Trials.finish("4242", "removed")
    send(pid, :tick)
    :sys.get_state(pid)
    assert_receive {:trial_closed, ^primary}, 1_000
    {:ok, nil} = TrialValkey.command(["GET", "trial:owner_session"])
  end

  test "chat is admitted off the socket loop, deduplicated by chat ID" do
    {:ok, generation} = Trials.add("4242")
    {:ok, _} = Trials.field("4242", "state", "receiving")

    for chat_id <- ["a", "b", "a"] do
      payload = %{"event" => %{"broadcaster_user_id" => "4242", "message_id" => chat_id}}
      TrialAdmission.submit(payload, %{broadcaster_id: "4242"}, generation)
    end

    settle_admission()
    assert {:ok, %{trials: [%{received: 2, failed: 0}]}} = Trials.list()
  end

  test "admission never counts chat for a channel that is not receiving" do
    {:ok, generation} = Trials.add("4242")

    TrialAdmission.submit(
      %{"event" => %{"message_id" => "a"}},
      %{broadcaster_id: "4242"},
      generation
    )

    settle_admission()
    assert {:ok, %{trials: [%{received: 0, failed: 0}]}} = Trials.list()
  end

  test "loads count every notification per trial channel like a shard socket" do
    {:ok, _} = Trials.add("4242")
    {:ok, _} = Trials.field("4242", "state", "receiving")
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    assert_receive {:trial_connected, primary, _url}, 1_000

    chat = fn id, chat_id ->
      frame("notification", %{
        subscription: %{type: "channel.chat.message"},
        event: %{broadcaster_user_id: id, message_id: chat_id}
      })
    end

    send(pid, {:fake_ws, primary, [chat.("4242", "a"), chat.("4242", "a"), chat.("9999", "b")]})

    assert %{
             slot: 0,
             owned: true,
             channels: 1,
             load: 2,
             loads: %{"4242" => 2},
             burst: 2,
             bursts: %{"4242" => 2}
           } = TrialReceiver.status(pid)
  end

  test "each socket slot subscribes only the channels assigned to it" do
    {:ok, _} = Trials.set_socket_target(2)
    {:ok, _} = Trials.add("4242")
    {:ok, _} = Trials.add("4343")
    {:ok, %{trials: rows}} = Trials.list()
    assert rows |> Enum.map(& &1.slot) |> Enum.sort() == [0, 1]

    zero = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    assert_receive {:trial_connected, _, _}, 1_000
    one = start_supervised!({TrialReceiver, [slot: 1, ws_module: Ingress.TrialFakeWS]})
    for _ <- 1..3, do: send(one, :tick)
    assert_receive {:trial_connected, _, _}, 1_000

    assert %{slot: 0, owned: true, channels: 1} = TrialReceiver.status(zero)
    assert %{slot: 1, owned: true, channels: 1} = TrialReceiver.status(one)
  end

  test "a disabled channel rejects a frame still present in the receiver snapshot" do
    {:ok, _} = Trials.add("4242")
    {:ok, _} = Trials.field("4242", "state", "receiving")
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    assert_receive {:trial_connected, primary, _url}, 1_000
    assert :sys.get_state(pid).rows["4242"].state == "receiving"

    {:ok, "disabled"} = Trials.set_enabled("4242", false)
    assert :sys.get_state(pid).rows["4242"].enabled == true

    payload = %{
      subscription: %{type: "channel.chat.message"},
      event: %{broadcaster_user_id: "4242", message_id: "after-disable"}
    }

    send(pid, {:fake_ws, primary, [frame("notification", payload)]})
    :sys.get_state(pid)
    settle_admission()
    assert {:ok, %{trials: [%{received: 0, enabled: false}]}} = Trials.list()
  end
end

defmodule Ingress.TrialAdmissionDropTest do
  use ExUnit.Case, async: false

  test "submitting with no admission worker running drops instead of blocking the socket" do
    refute Process.whereis(Ingress.TrialAdmission.name(0))

    assert :ok ==
             Ingress.TrialAdmission.submit(
               %{"event" => %{"message_id" => "a"}},
               %{broadcaster_id: "4242"},
               "1"
             )
  end
end
