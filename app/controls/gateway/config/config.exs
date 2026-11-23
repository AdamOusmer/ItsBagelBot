# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:device_id, :user_id, :event_id, :trace_id, :reason]

# Network listeners (HTTP/WS, NATS) stay off for plain `mix test`; integration
# tests start them explicitly against a test-local NATS and a test port.
if config_env() == :test do
  config :controls_gateway,
    server: false,
    metrics_flush_ms: 3_600_000,
    pull_expires_ns: 300_000_000,
    metrics_reporter: ControlsGateway.TestSupport.MetricsProbe
end
