# Sesame production capacity acceptance

This benchmark measures a production-shaped Sesame pod without publishing to
the real ingress or outgress lanes. It creates a temporary memory-backed stream
under `twitch.outgress.bench.*`, preloads realistic `!bench` chat commands, and
drains them through:

1. the production native-TLS hub JetStream path;
2. the fleet-owned native `nats.go` subscriber and `bus.ConsumeWeighted` autoscaler;
3. `engine.Pipeline` decoding, NATS replay identity, automod and custom-command dispatch;
4. Sonic outgress encoding and asynchronous JetStream PubAcks into the same
   isolated stream.

The production `worker_*` consumers, Twitch outgress subjects, and Sesame KEDA
metrics never see the test. The stream, consumer and pod are removed by cleanup
traps.

Run one two-CPU production-shaped pod on node3:

```sh
deploy/k8s/sesame-live-acceptance/run.sh
```

Override `NODE`, `MESSAGES`, `CHANNELS`, `NAMESPACE`, or `BENCH_IMAGE`. A
single channel measures the hottest ordered partition; many channels measure
aggregate fleet throughput. Run nodes
sequentially first; concurrent runs intentionally load the shared NATS and
NATS dependency and should only be used during a controlled load window.
