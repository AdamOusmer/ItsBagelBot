#!/usr/bin/env bash
# Production-shaped, three-node aggregate JetStream PubAck acceptance.
# It does not change node networking or the worker1 internet plan.
set -euo pipefail

namespace=${NAMESPACE:-production}
target_eps=${TARGET_EPS:-700000}
total_messages=${MESSAGES:-7000000}
payload_bytes=${PAYLOAD_BYTES:-256}
image=${BENCH_IMAGE:-alpine:3.22}
run_id=$(date -u +%Y%m%d%H%M%S)
stream="FLEET_700K_${run_id}"
subject="twitch.outgress.bench.fleet${run_id}"
binary="/tmp/nats-fleet-700k-${run_id}"
results_dir=$(mktemp -d)
nodes=(node2 node3 worker1)
pods=("nats-fleet-node2-${run_id}" "nats-fleet-node3-${run_id}" "nats-fleet-worker1-${run_id}")

# Match the intended five-ingress-pod shape: node2=1, node3=2, worker1=2,
# with two scheduler-local publisher connections per pod.
publishers=(2 4 4)
messages=(
  $((total_messages * 20 / 100))
  $((total_messages * 40 / 100))
  $((total_messages - total_messages * 20 / 100 - total_messages * 40 / 100))
)

stream_created=false

cleanup() {
  if [[ "$stream_created" == true ]]; then
    kubectl -n "$namespace" exec "${pods[1]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
      /tmp/nats-live-acceptance \
      -domain= \
      -stream "$stream" -subject "$subject" -create-stream=false -cleanup=true \
      -setup-only=true >/dev/null 2>&1 || true
  fi
  kubectl -n "$namespace" delete pod "${pods[@]}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -rf "$results_dir" "$binary"
}
trap cleanup EXIT INT TERM

create_pod() {
  local pod=$1 node=$2
  local overrides
  overrides=$(jq -nc \
    --arg pod "$pod" --arg node "$node" --arg image "$image" \
    '{spec:{nodeName:$node,restartPolicy:"Never",containers:[{
      name:$pod,image:$image,command:["sleep","1800"],
      env:[
        {name:"NATS_USER",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_USER"}}},
        {name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_PASSWORD"}}}
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

for i in "${!nodes[@]}"; do
  create_pod "${pods[$i]}" "${nodes[$i]}"
done
for pod in "${pods[@]}"; do
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
  kubectl -n "$namespace" cp "$binary" "$pod:/tmp/nats-live-acceptance"
done

kubectl -n "$namespace" exec "${pods[1]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
  /tmp/nats-live-acceptance \
  -domain= -placement-tag=nats-0 \
  -stream "$stream" -subject "$subject" -setup-only=true -cleanup=false >/dev/null
stream_created=true

echo "running ${total_messages} messages across node2/node3/worker1; target ${target_eps}/s"
pids=()
for i in "${!nodes[@]}"; do
  kubectl -n "$namespace" exec "${pods[$i]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
    /tmp/nats-live-acceptance \
    -domain= \
    -stream "$stream" -subject "$subject" -create-stream=false -cleanup=false \
    -producer-id="${nodes[$i]}" \
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
  ($r | map(.duration_ms) | max) as $duration |
  {
    target_messages_per_second:$target,
    acknowledged:$acked,
    conservative_duration_ms:$duration,
    aggregate_messages_per_second:($acked / ($duration / 1000)),
    nodes:$r,
    passed:(($acked / ($duration / 1000)) >= $target and ($r | all(.errors == 0 and .passed == true)))
  }
' "$results_dir"/*.json)
echo "$summary"
jq -e '.passed' <<<"$summary" >/dev/null
