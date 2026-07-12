# Live NATS acceptance test

This test measures the production-shaped JetStream publish/PubAck path directly
through the native-TLS hub. Leaves are RPC-only. It creates a unique
memory-backed stream on `twitch.outgress.bench.*`, a subject no production
stream or consumer owns, and deletes it on exit.

The credential must be allowed to manage JetStream and publish
`twitch.outgress.>`; production's `worker_bus` credential has exactly that
scope. The test requires `NATS_USER`, `NATS_PASSWORD`, and `NATS_CA` and refuses
to run without CA verification.

The defaults mirror one ingress pod: two publisher connections, 128-message
atomic commits, 200,000 messages, and 256-byte payloads. Use
`-mode=async` for the old per-message PubAck comparison. The temporary stream
enables both `AllowAtomicPublish` and NATS 2.14 `AllowBatchPublish`.

Build the static Linux binary, copy it into a temporary in-cluster pod holding
the scoped credential and fleet CA, and run:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o /tmp/nats-live-acceptance ./deploy/k8s/nats-live-acceptance

kubectl -n production cp /tmp/nats-live-acceptance <benchmark-pod>:/tmp/nats-live-acceptance
kubectl -n production exec <benchmark-pod> -- sh -c \
  'NATS_USER="$WORKER_NATS_USER" NATS_PASSWORD="$WORKER_NATS_PASSWORD" \
   NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance'
```

Acceptance gates:

- the hub endpoint negotiates verified TLS 1.2 or newer;
- all messages receive PubAcks and the error count is zero;
- PubAck p95 remains below 20 ms;
- no slow-consumer or quorum-loss messages appear in NATS logs.

The command prints machine-readable JSON and returns non-zero if the hub
loses or times out a PubAck, or exceeds the configurable `-max-p95` gate (20 ms
by default). Stream cleanup is enabled by default, including on benchmark
failure.

## Direct leaf RPC test

`rpc-leaf-direct.sh` creates a `USERS_RPC` responder on node2 and an
`ADMIN_RPC` requester on node3. It sends 100,000 cross-account requests through
the strict node-local leaf Services, then verifies:

- every request and reply crossed the direct leaf-cluster route;
- both client connections negotiated native TLS;
- the `ADMIN_RPC` and `USERS_RPC` hub leafnode counters did not move.

Run from the operator context:

```sh
deploy/k8s/nats-live-acceptance/rpc-leaf-direct.sh
```

Temporary pods are deleted by a trap on success, failure, or interruption.

## Three-node 700k gate

`fleet-700k.sh` builds the static binary and creates one temporary publisher pod
on node2, node3, and worker1. It opens 2/4/4 publisher connections, matching the
intended 1/2/2 ingress-pod placement, and drives one unsharded R1 memory stream.
The conservative aggregate rate uses the slowest node duration and must reach
700,000 acknowledged events/second with zero errors.

The test uses the node-local `nats` Service endpoint. The hub cluster pins BUS
to a dedicated route and enables adaptive S2 route compression, so worker1's
existing internet service is not changed. Run only during a controlled load
window; the temporary generators may each burst to four CPU cores.

```sh
deploy/k8s/nats-live-acceptance/fleet-700k.sh
```

Override `TARGET_EPS`, `MESSAGES`, `PAYLOAD_BYTES`, or `BATCH_SIZE` as needed.
The stream and all temporary pods are removed on success, failure, or interrupt.
