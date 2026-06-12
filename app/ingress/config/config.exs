import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:shard_id, :node]

config :new_relic_agent,
  logs_in_context: :direct

if config_env() == :test do
  # Tests exercise modules directly; do not start the cluster, NATS, or DB.
  config :ingress, server: false
end
