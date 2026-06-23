# NATS account isolation — secret & Doppler inventory

The fleet runs per-account isolation (see [nats-auth.conf](nats-auth.conf)): a
shared **BUS** account for the JetStream/event plane and one **`<SERVICE>_RPC`**
account per service for the request/reply + cache plane. Every runtime holds two
connections (BUS creds + per-service RPC creds) and prefers the node-local leaf.

This file is the operator checklist for the secrets that live **outside** the
repo. The bcrypt hashes are consumed by the `nats` and `nats-leaf` containers
through the `nats-auth-env` secret (`envFrom`); the plaintext lives in each
service's Doppler project.

## 1. Account ↔ user ↔ service ↔ connection

| Service | BUS user (NATS_USER) | RPC user (NATS_RPC_USER) | RPC account |
|---|---|---|---|
| users | `users_bus` | `users_rpc` | USERS_RPC |
| commands | `commands_bus` | `commands_rpc` | COMMANDS_RPC |
| modules | `modules_bus` | `modules_rpc` | MODULES_RPC |
| projector | `projector_bus` | `projector_rpc` | PROJECTOR_RPC |
| outgress | `outgress_bus` | `outgress_rpc` | OUTGRESS_RPC |
| worker | `worker_bus` | `worker_rpc` | WORKER_RPC |
| transactions | `transactions_bus` | — (BUS-only) | — |
| dashboard (console) | `dashboard_bus` | `dashboard_rpc` | DASHBOARD_RPC |
| admin (console) | `admin_bus` | `admin_rpc` | ADMIN_RPC |
| twitch-ingress | `twitch_ingress_bus` | `twitch_ingress_rpc` | TWITCH_INGRESS_RPC |

## 2. `nats-auth-env` secret keys (broker side)

All values are **bcrypt hashes** except the `*_REMOTE_URL_*` entries.

**BUS user hashes (10):**
`NATS_BCRYPT_USERS_BUS`, `NATS_BCRYPT_COMMANDS_BUS`, `NATS_BCRYPT_MODULES_BUS`,
`NATS_BCRYPT_TRANSACTIONS_BUS`, `NATS_BCRYPT_PROJECTOR_BUS`,
`NATS_BCRYPT_WORKER_BUS`, `NATS_BCRYPT_OUTGRESS_BUS`,
`NATS_BCRYPT_TWITCH_INGRESS_BUS`, `NATS_BCRYPT_DASHBOARD_BUS`,
`NATS_BCRYPT_ADMIN_BUS`

**RPC user hashes (9):**
`NATS_BCRYPT_USERS_RPC`, `NATS_BCRYPT_COMMANDS_RPC`, `NATS_BCRYPT_MODULES_RPC`,
`NATS_BCRYPT_PROJECTOR_RPC`, `NATS_BCRYPT_OUTGRESS_RPC`,
`NATS_BCRYPT_WORKER_RPC`, `NATS_BCRYPT_DASHBOARD_RPC`, `NATS_BCRYPT_ADMIN_RPC`,
`NATS_BCRYPT_TWITCH_INGRESS_RPC`

**System account (1):** `NATS_BCRYPT_SYS`

**Leaf link hashes — hub authorization, one per account (10):**
`NATS_BCRYPT_LEAF_BUS`, `NATS_BCRYPT_LEAF_USERS`, `NATS_BCRYPT_LEAF_COMMANDS`,
`NATS_BCRYPT_LEAF_MODULES`, `NATS_BCRYPT_LEAF_PROJECTOR`,
`NATS_BCRYPT_LEAF_OUTGRESS`, `NATS_BCRYPT_LEAF_WORKER`,
`NATS_BCRYPT_LEAF_DASHBOARD`, `NATS_BCRYPT_LEAF_ADMIN`,
`NATS_BCRYPT_LEAF_TWITCH_INGRESS`

**Leaf remote URLs — leaf side, one per account (10):** each embeds the
*plaintext* leaf password matching the hash above:
`NATS_LEAF_REMOTE_URL_BUS`, `NATS_LEAF_REMOTE_URL_USERS`,
`NATS_LEAF_REMOTE_URL_COMMANDS`, `NATS_LEAF_REMOTE_URL_MODULES`,
`NATS_LEAF_REMOTE_URL_PROJECTOR`, `NATS_LEAF_REMOTE_URL_OUTGRESS`,
`NATS_LEAF_REMOTE_URL_WORKER`, `NATS_LEAF_REMOTE_URL_DASHBOARD`,
`NATS_LEAF_REMOTE_URL_ADMIN`, `NATS_LEAF_REMOTE_URL_TWITCH_INGRESS`

Form: `nats-leaf://leaf_<account>:<plaintext>@nats.production.svc.cluster.local:7422`
e.g. `NATS_LEAF_REMOTE_URL_USERS=nats-leaf://leaf_users:<pw>@nats.production.svc.cluster.local:7422`

## 3. Per-service Doppler keys (app side)

Each service's Doppler project sets its own account creds; the app manifests
already pull them via `envFrom: secretRef` (DopplerSecret), so no manifest change
is needed for the credentials — only the keys below.

- **All services:** `NATS_USER` = `<service>_bus`, `NATS_PASSWORD` = the BUS
  plaintext (matches `NATS_BCRYPT_<SERVICE>_BUS`).
- **All except transactions:** `NATS_RPC_USER` = `<service>_rpc`,
  `NATS_RPC_PASSWORD` = the RPC plaintext (matches `NATS_BCRYPT_<SERVICE>_RPC`).
- `transactions` sets only `NATS_USER`/`NATS_PASSWORD`.

Leaf-first endpoint env is set in the manifests already: Go/console get
`NATS_LEAF_URL`/`NATS_HUB_URL`, ingress gets `NATS_LEAF_HOST`/`NATS_HUB_HOST`.

## 4. Generating bcrypt hashes

Use the `nats` CLI (one hash per user); store the plaintext in Doppler / the
remote URL and the printed hash in `nats-auth-env`:

```sh
# prints a bcrypt hash for the given password
nats server passwd
# or non-interactive with htpasswd (bcrypt, cost 11):
htpasswd -bnBC 11 "" "$PLAINTEXT" | tr -d ':\n' | sed 's/^\$2y/\$2a/'
```

## 5. Rollout (additive, hot-reloadable)

`nats-auth.conf` hot-reloads via the config-reloader SIGHUP, so accounts can be
staged before clients cut over.

1. Add all keys above to `nats-auth-env`; push config (Flux) → SIGHUP reload.
   The leaf opens one remote per account; verify all 10 links come up.
2. Ship the app code (already in this branch); `NATS_RPC_*` falls back to
   `NATS_USER`/`PASSWORD`, so apps keep working on their BUS user until RPC creds
   exist.
3. Populate `NATS_RPC_USER`/`NATS_RPC_PASSWORD` per service in Doppler; the
   Doppler operator restarts each app onto its RPC account. Roll one service at a
   time, watching RPC + the negative test (a service may not reach a subject it
   does not import).
4. The old single-account `BAGELBOT` user is fully removed — there is no shared
   fallback user left once every service is on its accounts.
