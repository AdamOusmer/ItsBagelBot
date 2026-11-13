# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:shard_id, :node]

config :os_mon, system_memory_high_watermark: 1.0

if config_env() == :test do
  config :ingress, server: false
end
