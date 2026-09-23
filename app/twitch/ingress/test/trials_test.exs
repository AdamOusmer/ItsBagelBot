defmodule Ingress.TrialsTest do
  use ExUnit.Case, async: false
  alias Ingress.{TrialValkey, Trials}

  setup do
    if System.find_executable("valkey-server") == nil do
      {:skip, "valkey-server is not installed"}
    else
      dir = Path.join(System.tmp_dir!(), "bagel-trial-#{System.unique_integer([:positive])}")
      File.mkdir_p!(dir)
      socket = Path.join(dir, "valkey.sock")
      pidfile = Path.join(dir, "valkey.pid")
      previous = System.get_env("VALKEY_ADDR")
      System.put_env("VALKEY_ADDR", "unix:" <> socket)

      {_output, 0} =
        System.cmd("valkey-server", [
          "--port",
          "0",
          "--unixsocket",
          socket,
          "--save",
          "",
          "--appendonly",
          "no",
          "--daemonize",
          "yes",
          "--pidfile",
          pidfile
        ])

      for _ <- 1..30 do
        if TrialValkey.command(["PING"]) != {:ok, "PONG"}, do: Process.sleep(20)
      end

      on_exit(fn ->
        _ = TrialValkey.command(["SHUTDOWN", "NOSAVE"])

        if previous,
          do: System.put_env("VALKEY_ADDR", previous),
          else: System.delete_env("VALKEY_ADDR")

        File.rm_rf!(dir)
      end)

      :ok
    end
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
    assert {:ok, %{active_count: 4, trials: rows}} = Trials.list()
    assert length(rows) == 4

    id = hd(rows).broadcaster_id
    assert {:ok, _} = Trials.field(id, "decoded", 3)
    assert {:ok, _} = Trials.field(id, "latency_samples", 2)
    assert {:ok, _} = Trials.field(id, "latency_total_ms", 25)
    generation = hd(rows).generation
    assert {:ok, 1} = Trials.display_name(id, generation, "Sample Streamer")
    assert {:ok, 0} = Trials.display_name(id, "stale", "Wrong Name")
    assert {:ok, %{trials: measured}} = Trials.list()

    assert Enum.any?(
             measured,
             &(&1.broadcaster_id == id and &1.decoded == 3 and
                 &1.average_processing_latency_ms == 12 and &1.display_name == "Sample Streamer")
           )

    assert {:ok, "pending"} = Trials.add(id)
    assert {:ok, "stopping"} = Trials.stop(id)
    assert {:ok, "stopping"} = Trials.stop(id)
    assert {:ok, 1} = Trials.finish(id, "removed")
    assert {:ok, %{active_count: 3, trials: history}} = Trials.list()
    assert Enum.any?(history, &(&1.broadcaster_id == id && &1.state == "removed"))
  end

  test "lease epoch fences a second owner" do
    assert {:ok, epoch} = Trials.acquire("owner-one")
    assert {:error, :owned} = Trials.acquire("owner-two")
    assert {:ok, 1} = Trials.renew("owner-one", epoch)
    assert {:ok, 0} = Trials.renew("owner-two", epoch)
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
  alias Ingress.{JSON, TrialReceiver, TrialValkey, Trials}

  setup do
    if System.find_executable("valkey-server") == nil do
      {:skip, "valkey-server is not installed"}
    else
      dir = Path.join(System.tmp_dir!(), "bagel-receiver-#{System.unique_integer([:positive])}")
      File.mkdir_p!(dir)
      socket = Path.join(dir, "valkey.sock")
      previous = System.get_env("VALKEY_ADDR")
      System.put_env("VALKEY_ADDR", "unix:" <> socket)
      Application.put_env(:ingress, :trial_test_pid, self())

      {_output, 0} =
        System.cmd("valkey-server", [
          "--port",
          "0",
          "--unixsocket",
          socket,
          "--save",
          "",
          "--appendonly",
          "no",
          "--daemonize",
          "yes"
        ])

      for _ <- 1..30 do
        if TrialValkey.command(["PING"]) != {:ok, "PONG"}, do: Process.sleep(20)
      end

      on_exit(fn ->
        _ = TrialValkey.command(["SHUTDOWN", "NOSAVE"])

        if previous,
          do: System.put_env("VALKEY_ADDR", previous),
          else: System.delete_env("VALKEY_ADDR")

        Application.delete_env(:ingress, :trial_test_pid)
        File.rm_rf!(dir)
      end)

      :ok
    end
  end

  defp frame(type, payload) do
    body =
      JSON.encode(%{metadata: %{message_type: type}, payload: payload}) |> IO.iodata_to_binary()

    {:frame, {:text, body}}
  end

  test "welcome, directed reconnect and revocation update receiver state" do
    assert {:ok, _} = Trials.add("4242")
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    assert_receive {:trial_connected, primary, _url}, 1_000

    send(
      pid,
      {:fake_ws, primary,
       [frame("session_welcome", %{session: %{id: "s1", keepalive_timeout_seconds: 30}})]}
    )

    assert :sys.get_state(pid).session_id == "s1"

    assert {:ok, _} = Trials.field("4242", "state", "receiving")
    assert {:ok, _} = Trials.field("4242", "session_id", "s1")
    assert {:ok, _} = Trials.field("4242", "subscription_id", "sub-1")

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
    assert {:ok, owner_session} = TrialValkey.command(["GET", "trial:owner_session"])
    assert String.ends_with?(owner_session, ":s2")
    assert_receive {:trial_closed, ^primary}, 1_000

    send(pid, :tick)
    :sys.get_state(pid)
    assert {:ok, %{trials: [%{state: "receiving", subscription_id: "sub-1"}]}} = Trials.list()

    send(
      pid,
      {:fake_ws, pending,
       [frame("revocation", %{subscription: %{condition: %{broadcaster_user_id: "4242"}}})]}
    )

    :sys.get_state(pid)
    assert {:ok, %{trials: [%{state: "failed", error: "subscription_revoked"}]}} = Trials.list()
  end

  test "socket opens with the first desired trial and closes after the last finishes" do
    pid = start_supervised!({TrialReceiver, [ws_module: Ingress.TrialFakeWS]})
    refute_receive {:trial_connected, _, _}, 100
    assert {:ok, _} = Trials.add("4242")
    send(pid, :tick)
    assert_receive {:trial_connected, primary, _}, 1_000
    assert {:ok, "stopping"} = Trials.stop("4242")
    assert {:ok, 1} = Trials.finish("4242", "removed")
    send(pid, :tick)
    :sys.get_state(pid)
    assert_receive {:trial_closed, ^primary}, 1_000
    assert {:ok, nil} = TrialValkey.command(["GET", "trial:owner_session"])
  end
end
