import Config

config :logger, :default_formatter,
  format: "$time [$level] $metadata$message\n",
  metadata: [:shard_id, :node]

# :forwarder rewrites each log line into a JSON blob carrying entity and trace
# linking metadata; the cluster Fluent Bit DaemonSet ships stdout to New Relic,
# which parses that JSON back into attributes. runtime.exs strips the plain-text
# formatter prefix in production so the line stays pure JSON.
config :new_relic_agent,
  logs_in_context: :forwarder

if config_env() == :test do
  # Tests exercise modules directly; do not start the cluster, NATS, or DB.
  config :ingress, server: false
end
