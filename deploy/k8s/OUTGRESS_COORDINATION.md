# Outgress coordination cutover

Outgress uses three replicated, file-backed NATS KV buckets: `outgress_rate`
(10-minute expiry), `outgress_batch` (2-minute expiry), and `outgress_pause`
(no expiry). Each is capped at 64 MiB. NATS remains a required dependency,
as it already carries outgoing messages. Loss of quorum fails closed.

Valkey loss no longer expires rate leases, invalidates the global pause
snapshot, or blocks batch checkpoints. Moderator-cache misses use the lower
non-mod allowance. Outgress can restart without Valkey once the durable pause
has been initialized. The first migration requires a successful Valkey pause
read; unknown pause state is never interpreted as permission to send.

This does not make Valkey-backed application features (timers, song queues,
live projections, enrollment management) available without their state store.
The token-source's existing cache/adoption and outage behavior is unchanged.

## First deployment

Do not roll old Valkey-limited and new NATS-limited replicas together: they
have independent rate budgets and checkpoints. Likewise, do not run a new
canary against real outgoing queues while old consumers are still active.

1. Build the merged outgress image and record its immutable digest.
2. Apply the reviewed NATS config maps. Confirm hot reload succeeds on all
   hub and leaf brokers. No credential rotation or broad wildcard grant is needed.
3. Record the current image and scaling settings. Pause the outgress KEDA
   scaler at zero replicas and verify every old outgress pod has terminated.
4. Wait at least 120 seconds after the last old pod stops. This drains the
   old batch-state lifetime and exceeds both chat/Helix rate windows. Chat
   queued during this maintenance window expires by its normal 5-second TTL.
5. Update the outgress manifest to the new digest, resume the scaler at its
   original minimum (3), and verify rollout readiness and queue consumption.
6. Confirm the `sending coordination ready: NATS quorum` startup log and
   absence of rate, batch, pause, or NATS-permission failures.

Do not delete the pause bucket to recover service. It is the authoritative
kill switch and must retain a deliberate pause across restarts. A stale or
unreadable NATS pause snapshot still blocks sending.

## Rollback

Stop/drain every new outgress replica before starting the old image. Wait
at least 120 seconds and copy the current NATS pause state to the legacy
Valkey pause control using the old management mechanism before resuming old
consumers. Do not assume the legacy pause value is current after this cutover.

## Verification

Run the race-enabled outgress, ratelimit, kvstate, valkey, bus and messaging
tests. `TestChatSendsWithValkeyUnavailable` uses an unreachable Valkey and a
stub Twitch transport; no chat is sent to a real channel. Rate tests share
one budget across twelve concurrent managers and reconstructed instances.
Batch tests cover checkpoint continuity and stale-owner fencing. Pause tests
cover outage, restart and missing-state rejection.

`TestSendingCoordinationQuorumIntegration` is opt-in with
`OUTGRESS_COORDINATION_TEST_URL` pointing to a disposable, loopback-only
three-node NATS fixture using the reviewed ACL and dummy `test-password`
credentials. Never point this test at production.
