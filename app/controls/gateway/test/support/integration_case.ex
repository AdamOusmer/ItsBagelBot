# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.IntegrationCase do
  @moduledoc """
  Boots separate local leaf and hub servers and a Cowboy listener per test.
  """
  use ExUnit.CaseTemplate

  alias ControlsGateway.TestSupport.TokenFixture
  alias Platform.Nats.TestSupport.NatsServer

  using opts do
    quote bind_quoted: [opts: opts] do
      use ExUnit.Case, opts
      alias ControlsGateway.TestSupport.{TokenFixture, WsClient}
    end
  end

  setup tags do
    {public_key, private_key} = TokenFixture.keypair()
    NatsServer.start_planes(if(tags[:jetstream], do: tags[:tmp_dir]))

    Application.put_env(:controls_gateway, :token_public_keys, %{"test" => public_key})
    http_port = NatsServer.free_port()
    start_supervised!({ControlsGateway.HttpServer, [port: http_port] ++ listener_opts(tags)})

    %{http_port: http_port, private_key: private_key, public_key: public_key}
  end

  defp listener_opts(%{routes: :desktop}),
    do: [routes: [{"/v1/connect", ControlsGateway.WsHandler, []}]]

  defp listener_opts(_tags), do: []
end
