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

Build the static Linux binary and copy it into a temporary in-cluster pod that
mounts the scoped credential and fleet CA. Credentials are transferred from
their existing Doppler projects directly into short-lived Kubernetes Secrets;
no credential belongs in the repository, command arguments, or shell history.

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o /tmp/nats-live-acceptance ./deploy/k8s/nats-live-acceptance

kubectl -n production cp /tmp/nats-live-acceptance <benchmark-pod>:/tmp/nats-live-acceptance
stream=LIVE_NATS_ACCEPTANCE_$(date -u +%Y%m%dT%H%M%S)
subject=twitch.outgress.bench.$(date -u +%Y%m%d%H%M%S)

# Operator setup identity from the temporary Secret: exact stream lifecycle only.
NATS_USER="$NATS_SETUP_USER" NATS_PASSWORD="$NATS_SETUP_PASSWORD" \
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
	  -domain= -stream "$stream" -subject "$subject" -setup-only -cleanup=false

# Runtime publisher identity from sesame-env: no JetStream API permissions.
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
	  -domain= -stream "$stream" -subject "$subject" \
	  -create-stream=false -cleanup=false

# Operator cleanup identity.
NATS_USER="$NATS_SETUP_USER" NATS_PASSWORD="$NATS_SETUP_PASSWORD" \
NATS_CA=/etc/nats-ca/ca.pem /tmp/nats-live-acceptance \
	  -domain= -stream "$stream" -subject "$subject" \
	  -create-stream=false -setup-only -cleanup=true
```

Acceptance gates:

- the hub endpoint negotiates verified TLS 1.2 or newer;
- all messages receive PubAcks and the error count is zero;
- under-load PubAck p95 remains at or below 20 ms and p99 at or below 2 ms;
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

This target is **not qualified on the current node2/node3/worker1 topology**.
The 2026-07-16 isolated NATS 2.14.3 matrix (native TLS, one broker and one
publisher pod per node, R3 memory stream, realistic 256-byte varied payloads,
official Orbit Fast-Ingest) found:

- forced `s2_fast`, node-local clients, flow 1000 / 8 outstanding is the
  throughput winner: about 75,000 events/s, zero final lag, no reconnects;
- enabling Tailscale's documented underlay UDP GRO forwarding and using
  byte-only rolling retention held the same throughput while reducing worst
  p99 to 118.834 ms and peak follower lag to 226;
- flow 128 / 4 outstanding reduced worst p99 to 80.559 ms and peak lag to 82,
  but throughput fell to 64,191 events/s;
- `s2_better` reached 69,636 events/s; `s2_best` could not finish the offered
  load before the bounded deadline;
- `s2_auto` left the low-RTT node2/node3 route uncompressed, accumulated a
  275 MB slow-consumer write, reconnected, peaked at 351,187 follower lag, and
  reached only 70,008 events/s;
- sending every publisher directly to the preferred leader reached only
  60,560 events/s; node-local clients remain faster.

Therefore neither the 90,000 events/s operating gate nor the 5 ms fleet PubAck
p99 gate is a configuration-only outcome for one R3 stream on these three
nodes. Do not promote this target contract based on the R1-era ~86k result: R1
does not pay R3 quorum replication. The next capacity step requires a second
stream/subject partition, a wired replacement for worker1, or another
same-datacenter voter. Raw isolated artifacts are retained under
`/tmp/nats-r3-isolated-2026071610*` on the qualification workstation.

`r3-120k.sh` is intentionally not part of the Kubernetes kustomization. Without
the exact confirmation token it only prints the plan and exits. With the token,
it creates one publisher pod on each of node2, node3, and worker1, plus separate
low-resource controller on node3 and one dedicated SLI pod on each publishing
node. Only the controller receives the short-lived `admin_bus` setup credential.
The checked-in ACL grants that identity create/delete/leader-stepdown/advisory
access only for `R3_SHADOW_BENCH`.

Every trial safely recreates that one stream with a unique subject. Cleanup
inspects the exact stream name and subject before deletion, so a concurrent or
leftover stream cannot be deleted by name alone. The runner discovers the live
NATS pod-to-node mapping, moves leadership away from worker1 if necessary,
publishes directly to the selected leader over native TLS, and aborts if
worker1 becomes leader. Worker1 must remain a current, zero-lag follower.

The sequence is:

1. Ramp the ordinary `nats.go` async path through 12,000, 30,000, 60,000, and
   90,000 events/second (10/25/50/75% of rated capacity), for one minute each.
   Any failure stops the run before 120,000 events/second is offered.
2. Calibrate at a bounded 120,000 events/second (never an unpaced burst): async,
   atomic batches 32/64/128 with four batches in flight per
   connection, and official Fast-Ingest flows 32/64/128 with two/four
   outstanding flow acknowledgements.
3. Select the lowest-tail-latency passing Elixir-compatible mode (`async` or
   `atomic`). Fast-Ingest records a paced 120,000/s comparison point but cannot
   qualify the Elixir ingress path.
4. Offer 126,000 events/second for five minutes and require at least 120,000
   acknowledged events/second.
5. Offer exactly 90,000 events/second for thirty minutes as the 75% operating
   soak and require at least 89,100 acknowledged events/second (99% delivery
   cadence, with all messages still acknowledged).
6. Require zero message/connection errors, p95 <= 20 ms and p99 <= 2 ms at both
   the 12k/s normal-load canary and higher offered load, broker CPU below 75%
   of its four-core limit, no stream-leader election advisory during load, all
   three peers current,
   follower lag returned to zero, and no queue, slow-consumer, write-deadline,
   or quorum error in NATS logs.

Each generator is hard-limited to one CPU and its client reconnect buffer is
disabled, so a bus interruption fails immediately instead of accumulating an
in-memory replay surge. A per-trial watchdog bounds every publish phase. The
two-second stall gate is applied independently to the maximum synchronous
under-load PubAck RTT and publisher completion progress. The async path drains
already-resolved futures while its bounded window is still being filled, so a
low-rate canary retains the full throughput window without manufacturing a
window-fill stall. Latency samples rotate across both connections on each node.

One immutable baseline and a run-global circuit breaker cover publisher pod
creation, every stream create/settle/leader move, all load, every stream delete,
and a zero-load cooldown. The circuit breaker requires:

- every production Deployment to stay fully available and every baseline pod
  UID/restart count to remain unchanged, with no rollout or scale change;
- all NATS and Valkey members to remain Ready with the same identities;
- all four production streams to keep the same leader and replica set, with
  every replica current, online, and at zero lag;
- the exact baseline production consumer identities and creation timestamps to
  remain present, with bounded lag and zero redelivery;
- no new broker slow consumers, meta backlog outside bounded stream lifecycle
  operations, or NATS queue/write/quorum errors;
- continuous passing side-effect-free RPC checks through the strict node-local
  leaf on node2, node3, and worker1; a read-only
  `twitch.ingress.admin.shards.get` check proving every desired WebSocket shard
  remains connected/bound/fresh with an unchanged session; and isolated TTL
  Valkey PING/master-SET/node-local-GET convergence measurements on every lane.
  Ordinary RPC and Valkey samples keep a 250 ms ceiling. The heavier read-only
  shard snapshot uses a separate 500 ms ceiling and is still included in the
  reported p95, p99, and maximum latency. Each lane separately enforces the
  historical RPC p99 of 8 ms after 330 samples, over a rolling nearest-rank
  window of the latest 1,100 RPC requests; that is independent of the 2 ms
  JetStream PubAck p99 gate. All three RPC windows must be armed and passing
  before the first temporary stream is created or any benchmark load begins.

On a lost exec stream or safety-monitor failure, the three publisher pods are
deleted and confirmed gone before the separately credentialed controller
deletes the exact-owned stream. This is subject and workload isolation on the
shared cluster, not a claim that the shadow stream uses separate NATS hardware.

Run only in a controlled window:

```sh
# Transfer the existing admin_bus identity from the admin/prd Doppler config
# without placing either value in command arguments or a checked-in file.
setup_secret=nats-bench-setup-$(date -u +%Y%m%d%H%M%S)
doppler run --project admin --config prd -- sh -c \
  'printf "NATS_USER=%s\nNATS_PASSWORD=%s\n" "$NATS_USER" "$NATS_PASSWORD"' | \
  kubectl -n production create secret generic "$setup_secret" \
    --from-env-file=/dev/stdin

# First prove the four-stage ramp and clean teardown only.
CONFIRM_R3_SHADOW=R3-120K R3_CANARY_ONLY=true \
  NATS_BENCH_SETUP_SECRET="$setup_secret" \
  deploy/k8s/nats-live-acceptance/r3-120k.sh

# Full qualification. OPERATING_SECONDS=3600 extends the 75% operating soak to
# one hour for the long-run stability gate; omit it for the 30-minute contract.
CONFIRM_R3_SHADOW=R3-120K OPERATING_SECONDS=3600 \
  NATS_BENCH_SETUP_SECRET="$setup_secret" \
  deploy/k8s/nats-live-acceptance/r3-120k.sh

kubectl -n production delete secret "$setup_secret"
```

This command uses a local static Go build and Kubernetes temporary pods; it
does not invoke Docker or any local container engine. It does not edit or apply
production manifests, server configuration, sysctls, stream contracts, or
autoscaling settings. Promotion of `TWITCH_INGRESS` to R3 is a separate PR
after this gate passes.

Only the dedicated node3 controller receives the temporary setup Secret. The
three publisher pods read only the BUS publisher keys from `sesame-env` by
default. The three SLI pods read the existing `ADMIN_RPC` keys from
`console-admin-env` and only the Valkey password from `sesame-env`; they receive
neither BUS publishing nor stream-management credentials. Override Secret
names only with `NATS_BENCH_PUBLISHER_SECRET` and
`NATS_BENCH_ADMIN_RPC_SECRET`.

Publishers use their node-local hub by default, matching the live Service
locality policy. `R3_PUBLISH_TARGET=preferred` is a comparison-only override;
it cannot qualify the node-local operating contract.

Each trial uses a shared start barrier and computes fleet throughput over the
earliest publisher start through the latest publisher finish, so delayed or
non-overlapping `kubectl exec` processes cannot inflate the rate. A production
health, topology, or watchdog failure terminates every generator promptly.
An isolated publisher gate failure lets the other bounded publishers exit before
cleanup, preventing orphaned remote processes. Stream deletion
is verified and retried; failure to confirm cleanup aborts the matrix. Raw node,
topology, cleanup, and summary JSON stays in
`/tmp/nats-r3-results-<run-id>` (or `R3_RESULTS_DIR`) for audit.

To qualify the node-local RPC health round-trip and Valkey paths without
creating a JetStream stream or any publisher pod, run the guarded SLI-only
mode. It warms all three lanes until the 330-sample RPC p99 gate is armed,
writes `sli-summary.json`, and then verifies cleanup:

```sh
CONFIRM_R3_SHADOW=R3-120K R3_SLI_ONLY=true \
  deploy/k8s/nats-live-acceptance/r3-120k.sh
```

This mode needs `console-admin-env` and the existing Valkey credential only;
it does not need the temporary JetStream setup Secret.

Compare retained R1/R3 and node-local/preferred summaries without touching the
cluster. The reporter exits non-zero until it sees a node-local R3 result that
passes the 90k/s, 30-minute, 2 ms loaded-p99, and 75%-CPU gates:

```sh
deploy/k8s/nats-live-acceptance/r3-matrix-report.sh \
  /tmp/nats-r3-isolated-* /tmp/nats-r3-results-*
```

Use `--report-only` to render an incomplete calibration matrix.

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
