# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:device_id]

# Network listeners (HTTP/WS, NATS) stay off for plain `mix test`; integration
# tests start them explicitly against a test-local NATS and a test port.
if config_env() == :test do
  config :controls_gateway, server: false
end
