# NATS pull-lane rollout

Operator runbook for the bus relanding: moving sesame's hot ingress lanes from
per-message explicit acknowledgement to the shared-durable **pull** consumer,
plus the broker change that ships beside it (the BUS account's own RAFT
egress).

Grants live in [nats-auth.conf](nats-auth.conf); the account model is in
[NATS-ACCOUNTS.md](NATS-ACCOUNTS.md). Run everything from the operator context
(`k8s-operator.tail451e6d.ts.net`).

The rollout is three stages and they are ordered by what breaks if they are not:

| Stage | Change | Delivery | Reversible by |
|---|---|---|---|
| 1 | ACL grants (`MSG.NEXT`, `TWITCH_INGRESS_STANDARD`), `cluster_traffic: owner` | git merge, then reload + hub roll | revert commit, roll again |
| 2 | Canary: `NATS_CONSUME_FLOW=on` on sesame | git commit | `NATS_CONSUME_FLOW=off` |
| 3 | `TWITCH_INGRESS_STANDARD` partition | service reconcile on image rollout | see section 6 |

Stage 1 changes no lane behaviour. Stage 2 is the only one chat can feel.
No new credentials ship with this rollout: every grant lands on existing users,
so nats-secrets.py is not involved.

## 1. Merge before Flux resumes

Flux's `apps` Kustomization regenerates the `nats-config` ConfigMap from
`deploy/k8s` on every reconcile (`configMapGenerator` in
[kustomization.yaml](kustomization.yaml), `disableNameSuffixHash: true`, so the
name is stable and the content is replaced in place). Git is therefore the only
durable copy of the broker config: anything applied to the live ConfigMap by
hand is overwritten at the next reconcile, at the latest on the 1h interval.

That is why `cluster_traffic: owner` and the new grants must be **on
main before `apps` is resumed or reconciled**. Resuming against an older conf
does not merely fail to add them, it removes what is live: the reloader sees the
content change, SIGHUPs, and the BUS account loses its own RAFT egress goroutine
so every stream and consumer append is serialized through the shared `$SYS`
sendq again. Recovering costs a full hub roll, because the accounts block is not
trusted to reload (see the comment on the `jetstream` block in nats-auth.conf).

```sh
git switch main && git pull --ff-only
git merge --no-ff feat/bus-relanding
git push
flux -n flux-system reconcile kustomization apps --with-source
```

Confirm the grants actually reached the ConfigMap before touching anything else:

```sh
kubectl -n production get cm nats-config -o jsonpath='{.data.auth\.conf}' \
  | grep -F '$JS.API.CONSUMER.MSG.NEXT.TWITCH_INGRESS'
```

If `apps` is suspended for an unrelated reason, resume it only after the merge:

```sh
flux -n flux-system resume kustomization apps
```

## 2. Leader spread: a manual policy, on purpose

Stream leaders drift on member restarts (elections are roulette, not
round-robin), and a member co-leading with TWITCH_INGRESS serves its other
lanes 25-40% slower (measured 2026-08-15). The assigned spread, set live that
night and still current:

| Stream | Leader |
|---|---|
| TWITCH_INGRESS | nats-1 |
| TWITCH_OUTGRESS | nats-2 |
| BAGEL_DATA, TWITCH_OUTGRESS_SYSTEM | nats-0 |

There is no automation, deliberately: at the current 60-80k operating load even
a fully piled-up member still clears the lanes, so drift is a growth concern
(relevant approaching ~130k aggregate), not an incident. After any hub roll,
re-check and restore the spread as the last step of the roll:

```sh
kubectl -n production exec nats-0 -c nats -- \
  wget -q -O- 'http://127.0.0.1:8222/jsz?streams=true' # leaders per stream
```

Restoring a leader means a `$JS.API.STREAM.LEADER.STEPDOWN.<stream>` request
with `{"placement":{"preferred":"nats-N"}}`. No standing identity carries that
verb (kept off the ACLs until automation is actually needed); use the SYS
account, or splice a temporary scoped user for the four streams the way the
bench grants were done, and remove it after.

## 3. Reload rules: what SIGHUPs and what has to roll

The config-reloader sidecar watches `/etc/nats/nats.conf`, `/etc/nats/auth.conf`
and the TLS cert, and SIGHUPs nats-server when any of their content changes. A
merged ACL edit therefore reloads on its own, once the kubelet has propagated
the new ConfigMap into the mounted volume (up to about a minute, not instant).

**Hot-reloads (no restart):** subject permissions added to or removed from an
existing user, new users whose `$NATS_BCRYPT_*` variable is already present in
`nats-auth-env`, TLS cert renewals.

**Requires a pod roll:**

- A user referencing a **new** `$NATS_BCRYPT_*` placeholder. `nats-auth-env` is
  consumed with `envFrom`, and environment is read once at container start, so
  the placeholder resolves to nothing until the pod restarts. (No user in this
  rollout does that; noted for the next one that does.)
- Anything in the accounts block's `jetstream` settings, including
  `cluster_traffic: owner`. Reload semantics there are not trusted.

Manual SIGHUP, for when the reloader is wedged or a reload needs forcing on one
member (`shareProcessNamespace` is on, and `--pid` writes the pidfile):

```sh
kubectl -n production exec nats-0 -c nats -- \
  sh -c 'kill -HUP $(cat /var/run/nats/nats.pid)'
```

Roll members one at a time and wait for readiness between them. Each restart
costs stream and consumer RAFT re-sync, and elections land leaders wherever the
returning member wins them; restore the spread per section 2 as the last step.
The `nats` StatefulSet's PDB allows one unavailable member.

## 4. The canary

Sesame is the canary because it is the only service that can be. pkg/bus admits
receipt-level consumption on the hot ingress lanes alone (`isHotIngressLane`),
and nothing else binds them: projector and outgress consume the stream lane, the
authz subjects, BAGEL_DATA and the outgress work queues, all of which the scope
guard declines and all of which are push consumers by construction. Setting the
flag on one of them would soak nothing. The manifests say so at the point an
operator would look ([projector.yaml](projector.yaml), [outgress.yaml](outgress.yaml)).

Pick a low-traffic window. The flip changes three things at once: the lanes bind
one fleet-wide pull durable instead of a per-pod push consumer, acknowledgement
becomes a batch floor rather than per message, and a handler error is scheduled
onto `TWITCH_INGRESS_RETRY` (which the same flag provisions and subscribes)
instead of NAKed.

**Before flipping**, check the existing durables. The pull consumer takes the
same name today's push consumer has (`worker_twitch_ingress_event_premium` and
`worker_twitch_ingress_event_standard`). The server refuses to convert a push
consumer to a pull consumer in place; the binding handles that with a
delete-and-recreate that carries the ack floor (bindPullConsumer in
pkg/bus/pull_consumer.go), so the flip is expected to log one replacement per
lane. Read the current shape first anyway, so the post-flip picture has a
before:

```sh
kubectl -n production port-forward pod/nats-0 8222:8222 &
curl -s 'http://127.0.0.1:8222/jsz?acc=BUS&consumers=true&config=true' \
  | jq '.account_details[].stream_detail[]
        | select(.name|startswith("TWITCH_INGRESS"))
        | .consumer_detail[]
        | {name, deliver: .config.deliver_subject, waiting: .num_waiting,
           delivered: .delivered.stream_seq, floor: .ack_floor.stream_seq,
           pending: .num_pending}'
```

Consumer state is served by the member that hosts it, so repeat for `nats-1` and
`nats-2` if a consumer is missing from the first answer. If the flip still fails on a
consumer error the fallback did not cover, delete the two durables by hand and
let the fleet recreate them (the lane's retention is 10s, so a fresh
`DeliverNew` durable skips at most that much; the lane's staleness policy
already says a late answer is worse than none).

**Flip:** edit [sesame.yaml](sesame.yaml), change `NATS_CONSUME_FLOW` from
`"off"` to `"on"`, commit, push. `NATS_CONSUME_MODE` is already pinned to `pull`.
Do not `kubectl set env`: Flux reverts it at the next reconcile.

```sh
flux -n flux-system reconcile kustomization apps --with-source
kubectl -n production rollout status deployment/sesame --timeout=10m
```

Watch, in this order:

1. **Lane delivery is non-zero.** Re-run the `jsz` query. `delivered` and
   `floor` must both climb, and `num_waiting` must be non-zero: a pull consumer
   parks fetch requests, a push consumer never does. `delivered` climbing while
   `floor` stands still is a lane taking messages and not acknowledging them.
2. **Fetches are not being refused.** A denied `MSG.NEXT` is silent on the
   client, so look for it on both sides:
   ```sh
   kubectl -n production logs -l app=sesame --tail=200 | grep -E 'lane fetch failed|lane floor ack failed'
   kubectl -n production logs nats-0 -c nats --tail=500 | grep -i 'Permissions Violation'
   ```
3. **The connection is the one you think it is.** The pull path names its
   connection `worker-pull` (the flow path names it `worker-flow`), so `connz`
   distinguishes a mixed rollout without reading any app log.
4. **The retry lane exists and is drained.** `TWITCH_INGRESS_RETRY` appears in
   `jsz` after the first pod converges. A consumer with rising `num_pending` and
   a still `floor` means retries are being scheduled and nobody is reading them.
5. **Backlog is not growing.** The KEDA triggers in sesame.yaml read
   `num_pending + num_ack_pending` on the two durables. Scaling up and staying
   up is the fleet telling you the lane is behind.
6. **Chat is answering.** The end of the pipeline is the only signal that covers
   handler behaviour under a floor ack: watch outgress send rate and a live
   channel.

Soak across at least one traffic peak and one sesame scale-up before treating
the shape as settled. The scale-up is the point of the change: pull divides the
lane across pods where the flow shape copied it to each.

## 5. Rollback, per stage

**Stage 2 (canary).** `NATS_CONSUME_FLOW=off` is the kill switch and it outranks
`NATS_CONSUME_MODE`, deliberately, so nobody has to know a second variable exists
during an incident. Set it back to `"off"`, commit, push, reconcile. The lanes
return to explicit per-message acks, the retry lane stops being provisioned and
stops being read, and the pull durables go inactive (they self-delete after the
5m inactive threshold). Nothing else has to be undone.

**Stage 1 (ACLs and cluster traffic).** Revert the commit on main and reconcile.
The ACL half reloads. Removing `cluster_traffic: owner` needs the same hub roll
adding it did, so treat the revert as a scheduled operation and not an
incident reflex. The added grants are consumer verbs on streams the account
already owns and carry no blast radius of their own, so there is rarely a reason
to revert stage 1 alone.

## 6. TWITCH_INGRESS_STANDARD: partition deploy ordering

The standard lane moves onto its own stream so the two hot lanes stop sharing
one serialized RAFT proposal loop. Nothing in `deploy/k8s` creates it: sesame
reconciles it at startup, so the stream appears when the image carrying the
partition rolls. The safety analysis for the subject move, including why
`TWITCH_INGRESS` must be narrowed before the new stream claims the subject and
why the reverse order deadlocks, lives with the partition change itself
(`bus.TwitchIngressStandardStream` in pkg/bus/streams.go). Read it there rather
than trusting a summary here.

Deploy-side ordering, which is this file's half:

1. **ACLs first, and they are already here.** `worker_bus` holds
   `STREAM.INFO/CREATE/UPDATE`, the consumer verbs, both `$JS.ACK` shapes,
   `MSG.NEXT` and `$JS.FC` on `TWITCH_INGRESS_STANDARD` as of stage 1. This is
   not optional ordering: a config push is a merge plus a reload, so a rollout
   that lands first fails its own `STREAM.CREATE` and every pod treats the failed
   initial provision as fatal. The grants are dormant until the stream exists.
2. **No other user gains verbs.** The partition moves only
   `twitch.ingress.event.standard`. Projector reads the stream lane and outgress
   reads the stream and authz subjects, all of which stay on `TWITCH_INGRESS`.
   If the partition's subject list ever grows, the consumer verb sets for
   `projector_bus` and `outgress_bus` move with it, in the same commit.
3. **The new stream needs a leader assignment.** The whole point of two
   streams is two leaders. After the partition's first rollout, check where the
   new stream's leader landed and move it per section 2 if the election paired
   it with `TWITCH_INGRESS`'s member — the two lane leaders on one box is the
   exact shape the partition exists to avoid. Add the stream to section 2's
   spread table in the same commit that creates it.
4. **KEDA triggers follow the consumer.** The `nats-jetstream` triggers in
   [sesame.yaml](sesame.yaml) name `stream: TWITCH_INGRESS` for both durables.
   The standard trigger has to name `TWITCH_INGRESS_STANDARD` once the consumer
   lives there, or it reads a consumer that no longer exists and KEDA serves the
   static `fallback` replica count while the HPA still reads healthy.
5. **Hub memory does not grow.** The new stream takes 640 MiB of the gigabyte
   `TWITCH_INGRESS` held, leaving it 384 MiB, so `max_mem` in
   [nats-server.conf](nats-server.conf) needs no change. Verify that assumption
   still holds against the spec before rolling, because raising a memory-backed
   stream's ceiling means raising `max_mem` on all three members at once.
