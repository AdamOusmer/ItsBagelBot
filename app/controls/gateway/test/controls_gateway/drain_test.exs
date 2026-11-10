# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.DrainTest do
  use ControlsGateway.IntegrationCase, async: false

  alias ControlsGateway.Drain

  @device_id "11111111-1111-4111-8111-111111111111"

  # draining? is a VM-wide persistent_term: never let it leak into other tests.
  setup do
    on_exit(fn -> Drain.reset!() end)
  end

  @tag timeout: 15_000
  test "drain fails readyz immediately, closes sockets with 1001, and rejects new connects", %{
    http_port: port,
    private_key: priv
  } do
    token =
      TokenFixture.sign(priv, TokenFixture.claims(%{"device_id" => @device_id}))

    {:ok, client} = WsClient.connect(port, [{"authorization", "Bearer " <> token}])

    assert Drain.ready?()
    task = Task.async(fn -> Drain.run() end)

    assert eventually(fn -> not Drain.ready?() end)

    assert WsClient.connect(port, [{"authorization", "Bearer " <> token}]) ==
             {:error, {:http_status, 503}}

    assert {:ok, [{:close, 1001, _reason}], _client} = WsClient.recv_frames(client, 10_000)
    assert Task.await(task, 10_000) == :ok
  end

  test "a socket that registers after drain enumerated is told to close" do
    assert Drain.run() == :ok

    state = %{
      device_id: @device_id,
      user_id: "user-1",
      exp_ms: System.system_time(:millisecond) + 60_000
    }

    ControlsGateway.WsHandler.websocket_init(state)

    assert_receive {:shutdown_close, _jitter_ms}
  end

  defp eventually(fun, attempts \\ 50) do
    cond do
      fun.() ->
        true

      attempts == 0 ->
        false

      true ->
        Process.sleep(10)
        eventually(fun, attempts - 1)
    end
  end
end
