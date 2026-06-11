import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:shard_id, :node]

if config_env() == :test do
  # Tests exercise modules directly; do not start the cluster, NATS, or DB.
  config :ingress, server: false
end
