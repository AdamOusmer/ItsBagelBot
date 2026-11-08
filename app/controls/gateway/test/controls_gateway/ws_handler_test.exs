# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.WsHandlerTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.Subjects

  @device_id "11111111-1111-4111-8111-111111111111"
  @other_device_id "22222222-2222-4222-8222-222222222222"

  test "rejects a connect attempt with no bearer token", %{http_port: port} do
    assert WsClient.connect(port, []) == {:error, {:http_status, 401}}
  end

  test "rejects a connect attempt with a garbage token", %{http_port: port} do
    headers = [{"authorization", "Bearer not-a-real-token"}]
    assert WsClient.connect(port, headers) == {:error, {:http_status, 401}}
  end

  test "rejects an expired token before upgrading", %{http_port: port, private_key: priv} do
    token = sign(priv, %{"exp" => System.system_time(:second) - 1})
    assert WsClient.connect(port, bearer(token)) == {:error, {:http_status, 401}}
  end

  test "accepts a valid token and echoes ping as pong", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = WsClient.send_text(client, ~s({"t":"ping"}))
    assert {:ok, [{:text, body}], _client} = WsClient.recv_frames(client)
    assert Jason.decode!(body) == %{"t" => "pong"}
  end

  test "closes with 4000 shortly after the token expires", %{http_port: port, private_key: priv} do
    now = System.system_time(:second)
    token = sign(priv, %{"device_id" => @device_id, "exp" => now + 1})
    {:ok, client} = WsClient.connect(port, bearer(token))

    assert {:ok, [{:close, 4000, _reason}], _client} = WsClient.recv_frames(client, 3_000)
  end

  test "closes a malformed (non-JSON) frame with 1007", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = WsClient.send_text(client, "not json")
    assert {:ok, [{:close, 1007, _reason}], _client} = WsClient.recv_frames(client)
  end

  test "closes a frame missing \"t\" with 1007", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = WsClient.send_text(client, ~s({"no_t":true}))
    assert {:ok, [{:close, 1007, _reason}], _client} = WsClient.recv_frames(client)
  end

  test "closes an oversized frame", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    oversized = Jason.encode!(%{"t" => "evt", "pad" => String.duplicate("x", 70_000)})
    client = WsClient.send_text(client, oversized)

    case WsClient.recv_frames(client, 3_000) do
      {:ok, [{:close, _code, _reason}], _client} -> :ok
      {:error, _reason, _client} -> :ok
    end
  end

  test "closes a connection that floods text frames", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)

    # The server may close mid-burst, so a failed send ends the burst without failing the test.
    client =
      Enum.reduce_while(1..500, client, fn _, acc ->
        try do
          {:cont, WsClient.send_text(acc, ~s({"t":"evt"}))}
        rescue
          MatchError -> {:halt, acc}
        end
      end)

    assert {:ok, frames, _client} = WsClient.recv_frames(client, 3_000)
    assert Enum.any?(frames, &match?({:close, 1008, _}, &1))
  end

  test "allows a steady text frame rate", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = Enum.reduce(1..20, client, fn _, acc -> WsClient.send_text(acc, ~s({"t":"evt"})) end)
    client = WsClient.send_text(client, ~s({"t":"ping"}))

    assert {:ok, [{:text, body}], _client} = WsClient.recv_frames(client)
    assert Jason.decode!(body) == %{"t" => "pong"}
  end

  test "a killed connection does not affect a sibling connection", %{
    http_port: port,
    private_key: priv
  } do
    {:ok, client_a} = connect_device(port, priv, @device_id)
    # A pong proves websocket_init has registered the handler, not just that
    # Cowboy sent the HTTP 101 response.
    client_a = ping_and_drain(client_a)
    [handler_a] = Registry.select(ControlsGateway.Connections, [{{:_, :"$1", :_}, [], [:"$1"]}])

    {:ok, client_b} = connect_device(port, priv, @other_device_id)
    client_b = ping_and_drain(client_b)
    pids = Registry.select(ControlsGateway.Connections, [{{:_, :"$1", :_}, [], [:"$1"]}])
    assert length(pids) == 2
    assert handler_a in pids

    ref = Process.monitor(handler_a)
    Process.exit(handler_a, :kill)
    assert_receive {:DOWN, ^ref, :process, ^handler_a, :killed}

    # The other connection stays usable and the listener accepts a new one.
    client_b = ping_and_drain(client_b)
    assert Process.whereis(ControlsGateway.HttpServer) |> Process.alive?()
    assert Process.whereis(ControlsGateway.Supervisor) |> Process.alive?()

    {:ok, client_c} = connect_device(port, priv, @device_id)
    client_c = WsClient.send_text(client_c, ~s({"t":"ping"}))
    assert {:ok, [{:text, body}], _client} = WsClient.recv_frames(client_c)
    assert Jason.decode!(body) == %{"t" => "pong"}

    WsClient.close(client_a)
    WsClient.close(client_b)
    WsClient.close(client_c)
  end

  test "a newer connection takes over and closes the older one", %{
    http_port: port,
    private_key: priv
  } do
    {:ok, older} = connect_device(port, priv, @device_id)
    older = ping_and_drain(older)

    {:ok, newer} =
      eventually_connect(fn -> connect_device(port, priv, @device_id) end)

    assert {:ok, [{:close, 4001, _reason}], _older} = WsClient.recv_frames(older, 3_000)

    newer = WsClient.send_text(newer, ~s({"t":"ping"}))
    assert {:ok, [{:text, body}], _newer} = WsClient.recv_frames(newer)
    assert Jason.decode!(body) == %{"t" => "pong"}
  end

  test "revocation closes the connection with 4003", %{http_port: port, private_key: priv} do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = ping_and_drain(client)

    eventually(fn -> ControlsGateway.Nats.publish(Subjects.revoked(@device_id), %{}) == :ok end)

    assert {:ok, [{:close, 4003, _reason}], _client} = WsClient.recv_frames(client, 3_000)
  end

  test "a command published to the device's subject arrives as a text frame", %{
    http_port: port,
    private_key: priv
  } do
    {:ok, client} = connect_device(port, priv, @device_id)
    client = ping_and_drain(client)

    command = %{"t" => "set_scene", "scene" => "brb"}

    eventually(fn ->
      ControlsGateway.Nats.publish(Subjects.device_cmd(@device_id), command) == :ok
    end)

    assert_frame_eventually(client, fn
      {:text, body} -> Jason.decode!(body) == command
      _other -> false
    end)
  end

  test "a device event is republished with the server-authenticated identity, not the client's",
       %{http_port: port, private_key: priv} do
    {:ok, pid} = wait_for_gnat()
    {:ok, _sid} = Gnat.sub(pid, self(), Subjects.device_evt(@device_id))

    {:ok, client} = connect_device(port, priv, @device_id)

    _client =
      WsClient.send_text(
        client,
        Jason.encode!(%{"t" => "button", "device_id" => "forged", "user_id" => "forged"})
      )

    assert_receive {:msg, %{topic: _topic, body: body}}, 3_000
    decoded = Jason.decode!(body)
    assert decoded["device_id"] == @device_id
    assert decoded["user_id"] == "user-1"
    assert decoded["t"] == "button"
  end

  defp connect_device(port, private_key, device_id) do
    token = sign(private_key, %{"device_id" => device_id})
    WsClient.connect(port, bearer(token))
  end

  defp sign(private_key, overrides) do
    ControlsGateway.TestSupport.TokenFixture.sign(
      private_key,
      ControlsGateway.TestSupport.TokenFixture.claims(overrides)
    )
  end

  defp bearer(token), do: [{"authorization", "Bearer " <> token}]

  defp ping_and_drain(client) do
    client = WsClient.send_text(client, ~s({"t":"ping"}))
    {:ok, [{:text, _pong}], client} = WsClient.recv_frames(client)
    client
  end

  defp wait_for_gnat(attempts \\ 50) do
    case Process.whereis(:gnat) do
      pid when is_pid(pid) ->
        {:ok, pid}

      nil when attempts > 0 ->
        Process.sleep(50)
        wait_for_gnat(attempts - 1)

      nil ->
        {:error, :not_connected}
    end
  end

  defp eventually(fun, attempts \\ 50) do
    cond do
      fun.() ->
        :ok

      attempts == 0 ->
        flunk("condition never became true")

      true ->
        Process.sleep(50)
        eventually(fun, attempts - 1)
    end
  end

  defp eventually_connect(fun, attempts \\ 50) do
    case fun.() do
      {:ok, client} ->
        {:ok, client}

      _error when attempts > 0 ->
        Process.sleep(50)
        eventually_connect(fun, attempts - 1)

      error ->
        error
    end
  end

  defp assert_frame_eventually(client, matcher, attempts \\ 60) do
    case WsClient.recv_frames(client, 500) do
      {:ok, frames, client} ->
        if Enum.any?(frames, matcher) do
          :ok
        else
          retry_frame(client, matcher, attempts)
        end

      {:error, :timeout, client} ->
        retry_frame(client, matcher, attempts)
    end
  end

  defp retry_frame(_client, _matcher, 0), do: flunk("expected frame never arrived")

  defp retry_frame(client, matcher, attempts),
    do: assert_frame_eventually(client, matcher, attempts - 1)
end
