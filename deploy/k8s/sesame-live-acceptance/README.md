# Sesame production capacity acceptance

This benchmark measures a production-shaped Sesame pod without publishing to
the real ingress or outgress lanes. It creates a temporary memory-backed stream
under `twitch.outgress.bench.*`, preloads realistic `!bench` chat commands, and
drains them through:

1. the production native-TLS hub JetStream path;
2. the fleet Watermill subscriber and `bus.ConsumeWeighted` autoscaler;
3. `engine.Pipeline` decoding, Valkey dedup, automod and custom-command dispatch;
4. Sonic outgress encoding and asynchronous JetStream PubAcks into the same
   isolated stream.

The production `worker_*` consumers, Twitch outgress subjects, and Sesame KEDA
metrics never see the test. Dedup keys use a two-minute TTL. The stream, consumer
and pod are removed by cleanup traps.

Run one two-CPU production-shaped pod on node3:

```sh
deploy/k8s/sesame-live-acceptance/run.sh
```

Override `NODE`, `MESSAGES`, `DEDUP`, `NAMESPACE`, or `BENCH_IMAGE`. Run nodes
sequentially first; concurrent runs intentionally load the shared NATS and
Valkey dependencies and should only be used during a controlled load window.
