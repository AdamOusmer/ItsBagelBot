# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:shard_id, :node]

config :new_relic_agent,
  logs_in_context: :forwarder

if config_env() == :test do
  config :ingress, server: false
end
