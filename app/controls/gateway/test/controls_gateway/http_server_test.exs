# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpServerTest do
  use ControlsGateway.IntegrationCase, async: false

  test "the listener has no connection cap" do
    assert :ranch.get_max_connections(ControlsGateway.HttpServer.listener()) == :infinity
  end
end
