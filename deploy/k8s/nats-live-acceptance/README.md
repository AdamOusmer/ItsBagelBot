# Live NATS acceptance test

This test measures the production-shaped JetStream publish/PubAck path directly
through the native-TLS hub. Leaves are RPC-only. It creates a unique
memory-backed stream on `twitch.outgress.bench.*`, a subject no production
stream or consumer owns, and deletes it on exit.

The benchmark publisher needs only its ordinary subject permission;
`worker_bus` deliberately has no stream-management rights. Create and delete
the unique temporary stream with a separate, short-lived operator credential,
which also subscribes to the per-stream
`$JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED.<stream>` advisory while validating
topology, then run the load phase with the worker credential and
`-create-stream=false -cleanup=false`. Never add stream management back to the
worker ACL for this harness. Every invocation requires `NATS_USER`,
`NATS_PASSWORD`, and `NATS_CA` and refuses to run without CA verification.

The defaults mirror one ingress pod: two publisher connections, bounded
official `nats.go` asynchronous PubAck windows, 200,000 messages, and a
65,536-entry ring of varied 256-byte EventSub-shaped payloads. `async` uses
`nats.go`; `atomic` and NATS 2.14 `fast` use the released
`github.com/synadia-io/orbit.go/jetstreamext` implementation. The harness does
not construct either protocol itself and never sends `Nats-Msg-Id`.

Build the static Linux binary, copy it into a temporary in-cluster pod holding
the scoped credential and fleet CA, and run the commands under `doppler run`.
The examples assume Doppler exposes `SETUP_NATS_USER`,
`SETUP_NATS_PASSWORD`, `WORKER_NATS_USER`, and `WORKER_NATS_PASSWORD`; no
credential belongs in the repository or shell history.

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o /tmp/nats-live-acceptance ./deploy/k8s/nats-live-acceptance

kubectl -n production cp /tmp/nats-live-acceptance <benchmark-pod>:/tmp/nats-live-acceptance
stream=LIVE_NATS_ACCEPTANCE_$(date -u +%Y%m%dT%H%M%S)
subject=twitch.outgress.bench.$(date -u +%Y%m%d%H%M%S)

# Operator setup identity: exact temporary stream lifecycle only.
doppler run -- sh -c 'NATS_USER="$SETUP_NATS_USER" NATS_PASSWORD="$SETUP_NATS_PASSWORD" \
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
  -domain= -stream "$1" -subject "$2" -setup-only -cleanup=false
' sh "$stream" "$subject"

# Runtime publisher identity: no JetStream API permissions.
doppler run -- sh -c 'NATS_USER="$WORKER_NATS_USER" NATS_PASSWORD="$WORKER_NATS_PASSWORD" \
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
  -domain= -stream "$1" -subject "$2" \
  -create-stream=false -cleanup=false
' sh "$stream" "$subject"

# Operator cleanup identity.
doppler run -- sh -c 'NATS_USER="$SETUP_NATS_USER" NATS_PASSWORD="$SETUP_NATS_PASSWORD" \
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
  -domain= -stream "$1" -subject "$2" \
  -create-stream=false -setup-only -cleanup=true
' sh "$stream" "$subject"
```

Acceptance gates:

- the hub endpoint negotiates verified TLS 1.2 or newer;
- all messages receive PubAcks and the error count is zero;
- under-load PubAck p95 remains at or below 20 ms and p99 at or below 50 ms;
- reconnect, disconnect, asynchronous-error, and timeout counts remain zero;
- no slow-consumer or quorum-loss messages appear in NATS logs.

The command prints machine-readable JSON and returns non-zero if the hub
loses or times out a PubAck, or exceeds the configurable latency gates. Stream
cleanup is enabled by default, including on benchmark failure. Create and
delete operations reject every name outside the explicit temporary prefixes,
so `TWITCH_INGRESS` cannot be targeted by this binary.

## R3 120k shadow qualification

`r3-capacity.json` is the future capacity contract: a three-replica,
memory-backed stream rated for 120,000 events/second and operated at 90,000
events/second (75%). It uses 1 GiB/5 minute retention, 400,000 messages per
subject, six publisher connections, 16,384 PubAcks per async window, and at
most 24 atomic batches in flight across the fleet.

`r3-120k.sh` is intentionally not part of the Kubernetes kustomization. Without
the exact confirmation token it only prints the plan and exits. With the token,
it creates one temporary pod on each of node2, node3, and worker1 and a fresh
`R3_SHADOW_*` stream for every trial. It discovers the live NATS pod-to-node
mapping, moves leadership away from worker1 if necessary, publishes directly
to the selected leader over native TLS, and aborts if worker1 becomes leader.
Worker1 must remain present as a current follower.

The sequence is:

1. Calibrate async, atomic batches 32/64/128 with four batches in flight per
   connection, and official Fast-Ingest flows 32/64/128 with two/four
   outstanding flow acknowledgements.
2. Select the fastest passing Elixir-compatible mode (`async` or `atomic`).
   Fast-Ingest records the broker ceiling but cannot qualify the ingress path.
3. Offer 126,000 events/second for five minutes and require at least 120,000
   acknowledged events/second.
4. Offer exactly 90,000 events/second for fifteen minutes as the 75% operating
   soak and require at least 89,100 acknowledged events/second (99% delivery
   cadence, with all messages still acknowledged).
5. Require zero message/connection errors, p95 <= 20 ms, p99 <= 50 ms, no
   stream-leader election advisory during load, all three peers current,
   follower lag returned to zero, and no queue, slow-consumer, write-deadline,
   or quorum error in NATS logs.

Run only in a controlled window:

```sh
# Transfer the short-lived setup identity from Doppler without placing its
# values in command arguments or a checked-in file.
doppler run -- sh -c \
  'printf "NATS_USER=%s\nNATS_PASSWORD=%s\n" "$SETUP_NATS_USER" "$SETUP_NATS_PASSWORD"' | \
  kubectl -n production create secret generic nats-bench-setup \
    --from-env-file=/dev/stdin

CONFIRM_R3_SHADOW=R3-120K \
  deploy/k8s/nats-live-acceptance/r3-120k.sh

kubectl -n production delete secret nats-bench-setup
```

This command uses a local static Go build and Kubernetes temporary pods; it
does not invoke Docker. It does not edit or apply production manifests, server
configuration, sysctls, stream contracts, or autoscaling settings. Promotion
of `TWITCH_INGRESS` to R3 is a separate PR after this gate passes.

Each trial uses a shared start barrier and computes fleet throughput over the
earliest publisher start through the latest publisher finish, so delayed or
non-overlapping `kubectl exec` processes cannot inflate the rate. Any publisher
or topology-process failure terminates its siblings promptly. Stream deletion
is verified and retried; failure to confirm cleanup aborts the matrix. Raw node,
topology, cleanup, and summary JSON stays in
`/tmp/nats-r3-results-<run-id>` (or `R3_RESULTS_DIR`) for audit.

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
on node2, node3, and worker1. It opens 2/2/2 publisher connections, matching the
current one-ingress-pod-per-node placement, and drives one unsharded R1 memory
stream.
The stream is pinned to `nats-0` on node3, matching the production ingress
placement. A temporary `nats-bench-setup` Secret (`NATS_USER` /
`NATS_PASSWORD`) supplies the short-lived setup identity, while the scoped
worker credential performs only the subject publish; leaves and the hub domain
import are intentionally bypassed. Remove the setup user and Secret after the
run.
The conservative aggregate rate uses the slowest node duration and must reach
700,000 acknowledged events/second with zero errors.

The test uses the node-local `nats` Service endpoint. The hub cluster pins BUS
to a dedicated route and enables adaptive S2 route compression, so worker1's
existing internet service is not changed. Run only during a controlled load
window; the temporary generators may each burst to four CPU cores.

```sh
deploy/k8s/nats-live-acceptance/fleet-700k.sh
```

Override `TARGET_EPS`, `MESSAGES`, or `PAYLOAD_BYTES` as needed.
The stream and all temporary pods are removed on success, failure, or interrupt.

## Dedup invariant

Every acceptance mode is structurally dedup-free. The binary exposes no flag
that can attach `Nats-Msg-Id`; the configured ten-second duplicate window is
therefore inert. Historical dedup A/B results remain useful evidence, but this
harness cannot accidentally restore that index cost.
