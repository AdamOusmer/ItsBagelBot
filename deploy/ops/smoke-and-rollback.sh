#!/usr/bin/env sh
set -eu

NAMESPACE="${NAMESPACE:-production}"
TIMEOUT="${TIMEOUT:-5m}"

workloads="
daemonset/console-dashboard
deployment/console-admin
daemonset/commands
daemonset/modules
daemonset/users
daemonset/transactions
daemonset/projector
daemonset/worker
daemonset/outgress
daemonset/twitch-ingress
daemonset/nats-leaf
"

smoke_url() {
  name="$1"
  url="$2"
  code="$(curl -ksS -o /tmp/smoke-body -w '%{http_code}' --max-time 10 "$url" || true)"
  if [ "$code" != "200" ]; then
    echo "smoke failed: $name returned HTTP $code from $url" >&2
    cat /tmp/smoke-body >&2 || true
    return 1
  fi
}

rollback_all() {
  echo "rolling back workloads in $NAMESPACE" >&2
  for workload in $workloads; do
    kubectl -n "$NAMESPACE" rollout undo "$workload" || true
  done
}

on_failure() {
  if [ "${ROLLBACK_ON_FAILURE:-true}" = "true" ]; then
    rollback_all
  fi
}

trap on_failure INT TERM ERR

pod_fetch() {
  pod="$1"
  container="$2"
  url="$3"
  kubectl -n "$NAMESPACE" exec "$pod" -c "$container" -- \
    bun -e "const r = await fetch('$url'); if (!r.ok) process.exit(1); console.log(await r.text())" >/dev/null
}

for workload in $workloads; do
  kubectl -n "$NAMESPACE" rollout status "$workload" --timeout="$TIMEOUT"
done

smoke_url dashboard-public "https://dashboard.itsbagelbot.com/healthz"

admin_pod="$(kubectl -n "$NAMESPACE" get pod -l app=console-admin -o jsonpath='{.items[0].metadata.name}')"
dashboard_pod="$(kubectl -n "$NAMESPACE" get pod -l app=console-dashboard -o jsonpath='{.items[0].metadata.name}')"
pod_fetch "$admin_pod" console-admin http://127.0.0.1:3000/readyz
pod_fetch "$dashboard_pod" console-dashboard http://127.0.0.1:3000/readyz

trap - INT TERM ERR
echo "rollout smoke passed"
