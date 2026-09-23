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

  test "concurrent admissions reserve no more than four slots and converge on duplicates" do
    outcomes =
      1..20
      |> Task.async_stream(fn id -> Trials.add(Integer.to_string(id)) end,
        max_concurrency: 20,
        timeout: 5_000
      )
      |> Enum.map(fn {:ok, value} -> value end)

    assert Enum.count(outcomes, &match?({:ok, _}, &1)) == 4
    assert Enum.count(outcomes, &(&1 == {:error, "full"})) == 16
    {:ok, %{active_count: 4, trials: rows}} = Trials.list()
    assert length(rows) == 4

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

    {:ok, "pending"} = Trials.add(id)
    {:ok, "stopping"} = Trials.stop(id)
    {:ok, "stopping"} = Trials.stop(id)
    {:ok, 1} = Trials.finish(id, "removed")
    {:ok, %{active_count: 3, trials: history}} = Trials.list()
    assert Enum.any?(history, &(&1.broadcaster_id == id && &1.state == "removed"))
  end

  test "lease epoch fences a second owner" do
    {:ok, epoch} = Trials.acquire("owner-one")
    assert {:error, :owned} = Trials.acquire("owner-two")
    {:ok, 1} = Trials.renew("owner-one", epoch)
    {:ok, 0} = Trials.renew("owner-two", epoch)
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

  alias Ingress.{JSON, TrialReceiver, TrialValkey, Trials}

  setup do
    if System.find_executable("valkey-server") do
      fixture = Ingress.TrialValkeyFixture.start()
      Application.put_env(:ingress, :trial_test_pid, self())
      start_supervised!({TrialValkey, []})
      assert :ok == Ingress.TrialValkeyFixture.await_ready()

      on_exit(fn ->
        Ingress.TrialValkeyFixture.stop(fixture)
        Application.delete_env(:ingress, :trial_test_pid)
      end)
    end

    :ok
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
end
