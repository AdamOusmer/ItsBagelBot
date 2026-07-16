#!/usr/bin/env bash
# Production-shaped, three-node aggregate JetStream PubAck acceptance.
# It does not change node networking or the worker1 internet plan.
set -euo pipefail

namespace=${NAMESPACE:-production}
target_eps=${TARGET_EPS:-700000}
total_messages=${MESSAGES:-7000000}
payload_bytes=${PAYLOAD_BYTES:-256}
image=${BENCH_IMAGE:-alpine:3.22}
# Temporary operator-managed identity used only to create/delete the unique
# benchmark stream. worker_bus deliberately has no stream-management ACL.
setup_secret=${NATS_BENCH_SETUP_SECRET:-nats-bench-setup}
run_id=$(date -u +%Y%m%d%H%M%S)
stream="FLEET_700K_${run_id}"
subject="twitch.outgress.bench.fleet${run_id}"
binary="/tmp/nats-fleet-700k-${run_id}"
results_dir=${FLEET_RESULTS_DIR:-"/tmp/nats-fleet-results-${run_id}"}
if ! mkdir "$results_dir"; then
  echo "FLEET_RESULTS_DIR must name a new, empty artifact directory: $results_dir" >&2
  exit 1
fi
nodes=(node2 node3 worker1)
pods=("nats-fleet-node2-${run_id}" "nats-fleet-node3-${run_id}" "nats-fleet-worker1-${run_id}")

# Match the current three-ingress-pod shape: one pod on each NATS node and two
# scheduler-local publisher connections per pod.
publishers=(2 2 2)
messages=(
  $((total_messages / 3))
  $((total_messages / 3))
  $((total_messages - 2 * (total_messages / 3)))
)

stream_created=false

setup_exec() {
  local pod=$1
  shift
  kubectl -n "$namespace" exec "$pod" -- sh -c \
    'NATS_USER="$NATS_SETUP_USER" NATS_PASSWORD="$NATS_SETUP_PASSWORD" NATS_CA=/etc/nats-ca/ca.pem exec "$@"' \
    sh "$@"
}

delete_stream() {
  if [[ $stream_created != true ]]; then
    return 0
  fi
  local attempt cleanup_log="$results_dir/stream-cleanup.log"
  for attempt in 1 2 3 4 5; do
    if setup_exec "${pods[1]}" /tmp/nats-live-acceptance \
      -domain= -replicas=1 -required-peers=1 \
      -stream "$stream" -subject "$subject" -create-stream=false -cleanup=true \
      -setup-only=true >>"$cleanup_log" 2>&1; then
      stream_created=false
      return 0
    fi
    sleep "$attempt"
  done
  echo "failed to verify deletion of fleet stream $stream after five attempts" >&2
  return 1
}

cleanup() {
  delete_stream || true
  kubectl -n "$namespace" delete pod "${pods[@]}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -f "$binary"
  echo "fleet artifacts retained in $results_dir"
}
trap cleanup EXIT INT TERM

create_pod() {
  local pod=$1 node=$2
  local overrides
  overrides=$(jq -nc \
    --arg pod "$pod" --arg node "$node" --arg image "$image" --arg setup_secret "$setup_secret" \
    '{spec:{nodeName:$node,restartPolicy:"Never",containers:[{
      name:$pod,image:$image,command:["sleep","1800"],
      env:[
        {name:"NATS_USER",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_USER"}}},
        {name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_PASSWORD"}}},
        {name:"NATS_SETUP_USER",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_USER"}}},
        {name:"NATS_SETUP_PASSWORD",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_PASSWORD"}}}
      ],
      volumeMounts:[{name:"fleet-ca",mountPath:"/etc/nats-ca",readOnly:true}],
      resources:{requests:{cpu:"100m",memory:"256Mi"},limits:{cpu:"4",memory:"1Gi"}},
      securityContext:{runAsUser:1000,runAsGroup:1000,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
    }],volumes:[{name:"fleet-ca",configMap:{name:"fleet-ca"}}],
    securityContext:{runAsNonRoot:true,runAsUser:1000,runAsGroup:1000,seccompProfile:{type:"RuntimeDefault"}}}}')
  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never \
    --overrides="$overrides" -- sleep 1800 >/dev/null
}

echo "building static acceptance binary"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$binary" ./deploy/k8s/nats-live-acceptance

if ! kubectl -n "$namespace" get secret "$setup_secret" >/dev/null 2>&1; then
  echo "missing temporary JetStream setup secret $setup_secret (see nats-live-acceptance/README.md)" >&2
  exit 1
fi

for i in "${!nodes[@]}"; do
  create_pod "${pods[$i]}" "${nodes[$i]}"
done
for pod in "${pods[@]}"; do
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
  kubectl -n "$namespace" cp "$binary" "$pod:/tmp/nats-live-acceptance"
done

stream_created=true
setup_exec "${pods[1]}" /tmp/nats-live-acceptance \
  -domain= -replicas=1 -required-peers=1 -placement-tag=nats-0 \
  -stream "$stream" -subject "$subject" -setup-only=true -cleanup=false >/dev/null

echo "running ${total_messages} dedup-free messages across node2/node3/worker1; target ${target_eps}/s"
start_ms=$((($(date +%s) + 5) * 1000))
pids=()
for i in "${!nodes[@]}"; do
  kubectl -n "$namespace" exec "${pods[$i]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
    /tmp/nats-live-acceptance \
    -domain= -replicas=1 -required-peers=1 \
    -stream "$stream" -subject "$subject" -create-stream=false -cleanup=false \
    -producer-id="${nodes[$i]}" \
    -start-at-unix-ms="$start_ms" \
    -messages="${messages[$i]}" -publishers="${publishers[$i]}" \
    -payload-bytes="$payload_bytes" \
    -latency-samples=0 -max-p95=1h \
    >"$results_dir/${nodes[$i]}.json" 2>"$results_dir/${nodes[$i]}.err" &
  pids+=("$!")
done

failed=0
for pid in "${pids[@]}"; do
  if ! wait "$pid"; then
    failed=1
  fi
done
if ((failed)); then
  for node in "${nodes[@]}"; do
    sed "s/^/${node}: /" "$results_dir/${node}.err" >&2 || true
  done
  exit 1
fi

summary=$(jq -s --argjson target "$target_eps" '
  [.[].results[0]] as $r |
  ($r | map(.acknowledged) | add) as $acked |
  ($r | map(.started_unix_ms) | min) as $started |
  ($r | map(.finished_unix_ms) | max) as $finished |
  ($finished - $started) as $duration |
  {
    target_messages_per_second:$target,
    acknowledged:$acked,
    conservative_duration_ms:$duration,
    aggregate_messages_per_second:($acked / ($duration / 1000)),
    nodes:$r,
    passed:(
      ($r | all(.started_unix_ms > 0 and .finished_unix_ms > .started_unix_ms)) and
      ($acked / ($duration / 1000)) >= $target and
      ($r | all(.errors == 0 and .passed == true))
    )
  }
' "$results_dir"/*.json)
echo "$summary"
jq -e '.passed' <<<"$summary" >/dev/null
delete_stream
