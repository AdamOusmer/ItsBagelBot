# nats-stress

Finds the **ceiling** and the **stable rate** of the rewritten NATS bus, and
validates the **idempotency guard** at that rate.

It is made of production parts, which is the only reason its numbers mean
anything:

| under test | what the rig uses |
| --- | --- |
| shadow stream | `bus.StreamSpec` + `bus.EnsureStreams` (pkg/bus/provision.go, modern JetStream API) |
| publisher | `bus.NewPublisherForStream` on the NATS 2.14 Fast-Ingest wire, `bus.PublishJSON` / `bus.PublishConfirmed` |
| consumer | `bus.NewFlowLaneSubscriber` (AckFlowControl, per-pod durable) behind `bus.ConsumeWeighted` |
| guard | `idempotency.NewTiered(LRU) → idempotency.NewValkeyStore(primary-pinned) → idempotency.Guard` |

Nothing in this directory reimplements a transport, and nothing production runs
is stubbed out inside the rig binary.

It is **not** part of `deploy/k8s/kustomization.yaml`, deliberately: Flux must
never reconcile a load generator back into existence.

## The shadow stream

`R3_SHADOW_BENCH`, subjects `twitch.outgress.bench.r3.>`, hot lane
`twitch.outgress.bench.r3.hot`. R3, memory-backed, `MaxBytes` 1 GiB,
`MaxMsgsPerSubject` 400k, batch publish on — the TWITCH_INGRESS shape, so the
operating point transfers.

The name and the subject prefix are pinned by the broker ACL, not chosen here:

- `admin_bus` holds `STREAM.CREATE/UPDATE/DELETE/LEADER.STEPDOWN` and `$JS.FC`
  for that exact stream name (deploy/k8s/nats-auth.conf, gated by
  `TestAdminBenchmarkStreamPermissionsAreExact`);
- `worker_bus`'s `twitch.outgress.>` publish grant is the only reason the data
  subjects are reachable at all.

That split is why the two roles carry two credentials: the consumer runs as
`admin_bus` (the only identity that may provision the stream and answer its flow
control) and the publisher as `worker_bus` (the only identity that may publish
the payloads). The consumer therefore starts first and provisions; the runner
waits for its `stream_ready` line before the publishers exist.

## Ceiling and stable rate

The publisher offers **open-loop** load: the offered count for each step is fixed
by the plan up front and the pacer keeps an absolute schedule, so falling behind
shows up as `achieved < offered` instead of quietly lowering demand.

- **step**: hold `-start-rate + n × -step-rate` for `-step-hold`, up to
  `-max-steps`. A step that cannot drain its offered count within
  `-step-overrun × -step-hold` is cut short and marked truncated.
- **breach**: `achieved < offered × -ceiling-ratio` (default 0.97) for the whole
  step, **or** failure rate above `-max-failure-rate` (default 0 — a rate only
  reachable by dropping messages is not a rate).
- **ceiling**: the best rate any step actually sustained, including the step that
  breached.
- **soak**: automatically at `ceiling × -stable-fraction` (default 0.85) for
  `-soak` (default 5m), asserting zero cohort failures, a flat lag curve and a
  p99 within `-latency-tolerance` of the ramp baseline.

Latency is publish→PubAck, sampled (`-latency-sample-every`, default every 512th
message) through `bus.PublishConfirmed`: that call rides the same cohort as
everything around it and simply waits for that cohort's acknowledgement. Timing
every message would turn the open-loop publisher into a closed-loop one and cap
the measured rate at one cohort round trip per lane.

## Guard validation

`-dup-fraction` (default 1%) of events are published **twice under one event id**
— half back-to-back, half after `-dup-delay` — and an equally sized control set
is guard-sampled but never duplicated. Everything else is firehose and never
touches Valkey.

The guard keys on the envelope's event id, not on `Message.UUID`: pkg/bus stamps
a fresh NUID per publish, so the two copies arrive under two different UUIDs and
a UUID-keyed guard could not recognise them as one event. `idempotency.KeyFunc`
exists for exactly this — the stable identity of an event is a domain question.

Verdicts are classified **after** an event's window has rotated out, never on
arrival: the guard's claim-before ordering means the second copy can learn it is
a duplicate before the first copy has recorded that it applied, and classifying
at arrival would race that window and report healthy deduplication as a false
positive.

| counter | meaning |
| --- | --- |
| `dups_caught` | injected duplicate suppressed — the guard working |
| `dups_missed` | injected duplicate applied twice — **must be 0** |
| `false_positives` | suppressed an event that had never been applied — **must be 0** |
| `control_redeliveries_caught` | natural JetStream redelivery caught (healthy) |
| `control_double_applied` | natural redelivery applied twice — **must be 0** |
| `dups_unobserved` | only one copy arrived; shrinks the sample, not a defect |

Claim prefix defaults to `stress:seen:<pod>:`. Two reasons, both load-bearing:
the root is never `sesame:seen:`, or a bench claim could collide with a live one
on the same primary; and the per-pod suffix exists because every consumer pod
owns its own flow consumer and receives the **whole** lane, so a shared namespace
would make a cross-pod claim indistinguishable from an injected duplicate. Pass
`-guard-prefix stress:seen:` to measure the shared-claim shape instead — the
Valkey load is identical either way.

**Expected Valkey load at the default flags**: ~2% of distinct events are
guard-sampled, and the per-pod LRU absorbs the second copy of every treatment
event, so one `SET NX` lands per distinct sampled event — about **1.2k claims/s
per consumer pod at 60k msg/s delivered**, ~2.4k/s across two pods. Against the
~250k ops/s the fleet primary measured, under 1%.

## Output

Both roles write newline-delimited JSON to **stdout** and nothing else; zap logs
go to **stderr**. Line kinds: `tick` (periodic), `step` (per ramp step),
`stream_ready`, `summary` (terminal). `-role report` merges captured summaries
into one verdict and exits non-zero when it fails; it is a pure function of the
two roles' summaries, so a captured log can be re-judged without the cluster.

## Running

Build and run are separate steps. Build first, from the repository root:

```sh
podman build -t ghcr.io/adamousmer/itsbagelbot/nats-stress:local \
  -f deploy/k8s/nats-stress/Containerfile .
```

or let `.github/workflows/publish-images.yml` build it (`nats-stress` is
path-mapped on `deploy/k8s/nats-stress/*` and on any `pkg/*` change) and take the
digest from the run.

Then, with the digest:

```sh
export PATH=$HOME/sdk/go1.26.0/bin:$PATH
CONFIRM_LIVE_NATS_STRESS=I-understand-this-loads-live-NATS \
BENCH_IMAGE=ghcr.io/adamousmer/itsbagelbot/nats-stress@sha256:<64 hex> \
  ./deploy/k8s/nats-stress/run.sh
```

Knobs (all environment variables, all with defaults):
`PUBLISHER_REPLICAS=2 CONSUMER_REPLICAS=1 LANES=8 PAYLOAD_BYTES=512
START_RATE=20000 STEP_RATE=10000 STEP_HOLD=30s MAX_STEPS=20 SOAK=5m
DUP_FRACTION=0.01 ROUTINES=100 GUARD_TTL=2m`.

The runner applies the consumer, waits for the stream, applies the publishers,
collects both logs, prints the verdict, and removes the Deployments **and the
shadow stream** on exit — including on failure.

Re-judge a captured run without touching the cluster:

```sh
go run ./deploy/k8s/nats-stress -role report publisher.jsonl consumer.jsonl
```

## Notes

- `R3_SHADOW_BENCH` is the same name the older
  `deploy/k8s/nats-live-acceptance/r3-120k.sh` harness uses. Never run both at
  once; this rig reconciles the stream to its own spec and deletes it on exit.
- Unit tests cover the rig's own logic (ramp scheduler, ceiling detector,
  sequence and duplicate accounting, report merge, pacer, payload sizing) with
  fakes and need no broker: `go test -race ./deploy/k8s/nats-stress`.
