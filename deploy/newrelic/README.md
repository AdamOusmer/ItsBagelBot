# New Relic observability (account-side config as code)

New Relic has two halves. The **agents** (metrics, logs, kube-state) ship from
the `nri-bundle` HelmRelease under Flux — see
[`deploy/flux/clusters/production/newrelic.yaml`](../flux/clusters/production/newrelic.yaml)
and the log pipeline in
[`deploy/infra/cluster/newrelic-logging.yaml`](../infra/cluster/newrelic-logging.yaml).

This directory owns the **other half** — the config that lives inside the New
Relic account and has no Kubernetes CRD: alert-condition thresholds, notification
destinations / channels / workflows, and the Fleet dashboard. It is reconciled
by an idempotent NerdGraph script, not by Flux (there is nothing for Flux to
apply — the target is the New Relic API, not the cluster).

## Layout

| File | What it defines |
|------|-----------------|
| `provision.py` | Idempotent reconciler. Reads the definitions, converges account `3823179`. |
| `definitions/alerts.json` | Desired `nrql` / `enabled` / `terms` per alert condition, keyed by policy + condition **name**. |
| `definitions/notifications.json` | Email + Discord destinations, channels, and the `BagelBot alerts` workflow. |
| `definitions/discord_payload.json.tmpl` | Discord embed template (Handlebars). `__ROLE_ID__` is substituted from Doppler. |
| `definitions/dashboard.json` | The `BagelBot Fleet` dashboard (5 pages), exact widget/layout spec. |

## Secrets (Doppler `infra` / `prd_newrelic`)

Nothing sensitive is committed. The script pulls:

- `NEWRELIC_USER_API_KEY` — NerdGraph user key.
- `DISCORD_ALERT_WEBHOOK_URL` — Discord incoming-webhook URL.
- `DISCORD_ALERT_ROLE_ID` — Discord role pinged in alert embeds.

## Usage

```bash
# preview what would change, no writes
python3 deploy/newrelic/provision.py --dry-run

# reconcile everything
python3 deploy/newrelic/provision.py

# one section only
python3 deploy/newrelic/provision.py --only notifications
```

Re-running is safe: objects are matched by name and edited in place, never
duplicated. A clean tree prints `30 in sync, 0 changed` for alerts and
`ok` for every destination/channel.

## Notes / gotchas

- **Alert conditions are not created here.** The two policies (`Golden Signals`,
  `Kubernetes alert policy`) and their conditions come from New Relic's guided
  installs. This script only reconciles the fields we deliberately set. A
  condition in the definitions but absent from the account is reported as
  `MISSING`, never recreated — a renamed upstream condition surfaces loudly
  instead of spawning a duplicate.
- **`Low Application Throughput` is intentionally disabled** (`enabled: false`).
  Bursty Twitch traffic drops to zero between streams, so a low-throughput
  baseline fired hundreds of false incidents. Do not re-enable without a
  per-stream signal.
- **Etcd conditions are disabled** — k3s uses kine/sqlite, so `K8sEtcdSample`
  never has data.
- **Discord embed color** is chosen in the template: green when the incident is
  closed, otherwise by priority (critical red / high orange / medium yellow /
  else blue). The `{{else}}` fallbacks must stay — a color that renders empty is
  invalid JSON and Discord drops the alert silently.
- The role ping lives in the embed `content` (mentions do not fire from inside
  an embed) with `allowed_mentions.roles` set.
