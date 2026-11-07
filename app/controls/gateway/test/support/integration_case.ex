# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.IntegrationCase do
  @moduledoc """
  Boots a real local nats-server, a real Gnat connection and a real Cowboy
  listener per test, torn down automatically by ExUnit's test supervisor.
  """
  use ExUnit.CaseTemplate

  alias ControlsGateway.TestSupport.{NatsServer, TokenFixture}

  using opts do
    quote bind_quoted: [opts: opts] do
      use ExUnit.Case, opts
      alias ControlsGateway.TestSupport.{TokenFixture, WsClient}
    end
  end

  setup do
    {public_key, private_key} = TokenFixture.keypair()
    nats_port = NatsServer.free_port()
    nats_server = NatsServer.start(nats_port)
    http_port = NatsServer.free_port()

    nats_settings = [%{host: "127.0.0.1", port: nats_port, no_responders: true}]
    Application.put_env(:controls_gateway, :nats, nats_settings)
    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})

    start_supervised!(
      {Gnat.ConnectionSupervisor,
       %{name: :gnat, backoff_period: 300, connection_settings: nats_settings}}
    )

    await_gnat_connected()
    start_supervised!({ControlsGateway.HttpServer, port: http_port})

    on_exit(fn -> NatsServer.stop(nats_server) end)

    %{http_port: http_port, private_key: private_key, public_key: public_key}
  end

  defp await_gnat_connected(attempts \\ 50) do
    cond do
      attempts == 0 ->
        raise "gnat did not connect to the test nats-server"

      is_pid(Process.whereis(:gnat)) ->
        :ok

      true ->
        Process.sleep(50)
        await_gnat_connected(attempts - 1)
    end
  end
end
