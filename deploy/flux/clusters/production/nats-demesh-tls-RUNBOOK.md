# NATS de-mesh + native TLS + R1 firehose — deploy runbook

Goal: a single unsharded `TWITCH_INGRESS` stream sustains ~150k ev/s by removing
the taxes on an **ack-bound** firehose producer (ceiling = `max_pending /
ack_latency`): R3 RAFT consensus, the leaf forwarding hop, and the Linkerd sidecar.
Wire encryption moves from the mesh to NATS-native TLS (cert-manager) over the
tailnet. See the design in `~/.claude/plans/staged-squishing-hellman.md`.

The manifest/code changes reach prod via Flux on merge to `main`. The steps below
are the ordering + the manual `kubectl`/`nats` ops Flux does not perform. **Do them
in order** — TLS must be live and verified before the mesh comes off.

## The one hard constraint: the TLS cutover is atomic

A bare `tls { }` block on the NATS client listener makes TLS **required**
(`tls_required` in server INFO). The moment the NATS pods reload that config,
every client must present TLS + trust the fleet CA (`NATS_CA_PEM`) or it cannot
connect. There is no half-TLS window. So the NATS TLS config and the app
`NATS_CA_PEM` env must land **together**, and a brief reconnect blip is expected as
pods roll (the fleet tolerates it: infinite reconnect + the 32MB reconnect buffer +
the perishable 5s/5m streams).

The client code is safe to ship early: `pkg/bus` only enables TLS when
`NATS_CA_PEM` is set, and the console/ingress do the same — so merging the code
before the env/config does nothing until the CA is present.

## Phase 1 — R1 firehose (no topology change, do first)

`pkg/bus/provision.go` now declares + **enforces** replicas (streamMatches compares
them; UpdateStream converges). On the new image the guardian converges the live
stream automatically. To do it explicitly / immediately:

```
nats stream update TWITCH_INGRESS --replicas 1     # R3 -> R1 (the throughput win)
nats stream update TWITCH_OUTGRESS_SYSTEM --replicas 3   # ensure the durable control lane is R3
nats stream info TWITCH_INGRESS --json | jq '.config.num_replicas'   # -> 1
```

Soak here first (see Verification): R1 + hub-direct (Phase 2, ships with the same
`pkg/bus`) alone may hit the target with the mesh still on.

## Phase 2 — hub-direct consumers (ships with Phase 1 code)

`busURL` is now hub-first via `NATS_HUB_URL`; the manifests set
`NATS_HUB_URL=nats://nats:4222`. Nothing manual — verify the leaf sheds the
firehose (its CPU/RSS drops; hub `connz` shows the consumer inboxes).

## Phase 3 — cert-manager PKI + NATS TLS (atomic; see the constraint above)

1. Merge the `deploy/infra/cert-manager` + `deploy/infra/pki` + their Flux
   Kustomizations. **Confirm the chart version pins** in
   `deploy/infra/cert-manager/release.yaml` are current before merge.
2. Wait for the PKI to be ready (the `apps` Kustomization dependsOn `pki`, so it
   blocks until these exist):
   ```
   kubectl -n cert-manager get pods                          # cert-manager + trust-manager Running
   kubectl -n production get certificate                     # nats-hub-tls / nats-leaf-tls / console-* = Ready
   kubectl -n production get configmap fleet-ca -o jsonpath='{.data.ca\.pem}' | head -c 40   # CA present
   kubectl -n production get secret nats-hub-tls nats-leaf-tls
   ```
3. The same merge carries the NATS `tls{}` config + every app's `NATS_CA_PEM`. The
   config-reloader SIGHUPs NATS to TLS; apps roll with the CA. Expect a short
   reconnect blip. Confirm TLS is live and auth is healthy:
   ```
   kubectl -n production exec nats-0 -c nats -- wget -qO- localhost:8222/connz | grep -c tls_version   # > 0
   ```
4. Functional round-trip (a `!`command + an automod line → bot replies).

## Phase 4 — console HTTPS (the de-mesh gate)

Ships with Phase 3 (same CA). `console-dashboard` now serves HTTPS; Traefik
re-encrypts via the `console-dashboard-transport` ServersTransport.
- `console-admin` is intentionally left HTTP: it is fronted by the **Tailscale**
  operator Ingress (tailnet WireGuard end-to-end), not Traefik, and stays meshed —
  so it has no plaintext-behind-Linkerd hop. Serving HTTPS there would break the
  ts-Ingress HTTP backend. (If you later want admin Linkerd-independent too, wire
  Tailscale-Ingress backend-TLS separately.)
- Gate check before Phase 5: load `https://dashboard.itsbagelbot.com`; confirm the
  Traefik→pod hop is TLS (backend :3000 HTTPS, cert chains to the fleet CA). If you
  want proof it no longer needs the mesh, temporarily `linkerd uninject` one
  console-dashboard pod and confirm it still serves.

## Phase 5 — de-mesh NATS (only after Phase 4 is green)

Ships the `linkerd.io/inject: disabled` on nats/nats-leaf + `skip-outbound-ports:
"4222"` on the apps. Confirm:
```
linkerd stat deploy -n production | grep -i nats || echo "nats no longer meshed (expected)"
kubectl -n production get pod -l app=nats -o jsonpath='{.items[0].spec.containers[*].name}'   # no linkerd-proxy
```
If a client cannot connect after this, it is almost always a missing/renamed
`NATS_CA_PEM` or a SAN mismatch — check the pod logs for a TLS handshake error, not
firewalld/mesh.

## Phase 6 — buffers (with or after Phase 5)

`max_pending 64MB` (hub), `ReconnectBufSize 32MB` (client) ship in the manifests/
image. The node sysctl (`99-nats-rpc.conf`, rmem/wmem 16MB) is applied by the
ansible base role — **node-level, not Flux**: run the base role (or
`sysctl --system`) on each NATS node. Then push `publish_max_pending` (ingress
`Config`) up only if the soak shows sustained `Nats/PublishInflight` saturation
without `Nats/PublishOverloaded`.

## Verification (soak ≥ 15–30 min, per the existing worker1 runbook Phase E)

- publish errors == 0, `Nats/PublishOverloaded` == 0, dispatcher drops == 0
- PubAck p50/p95/p99 (`Nats/PublishInflight` bounded) — should drop sharply vs R3
- consumer lag flat: `nats consumer info TWITCH_INGRESS worker_twitch_ingress_event_standard`
- leaf CPU/RSS low (Phase 2), hub CPU absorbs the firehose w/o a proxy (Phase 5)
- `nats server report jetstream` — no leader flapping (meta group only; stream R1)

Load harness: `pkg/bus/bustest/publisher.go`, or ride an organic peak.

## Rollback (per phase, independent)

- Phase 1: `nats stream update TWITCH_INGRESS --replicas 3`.
- Phase 5 (de-mesh): `git revert` the inject/skip-outbound change → Flux re-meshes.
- Phase 3/4 (TLS): `git revert` the TLS config + `NATS_CA_PEM` + console HTTPS
  together (they are atomic). cert-manager/trust-manager can stay installed inertly.
- Streams re-provision themselves via `EnsureStreams`; the firehose is perishable,
  no replay to restore.
