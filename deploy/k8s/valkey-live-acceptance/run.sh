#!/usr/bin/env bash
# Fleet-wide production Valkey p99 acceptance. Temporary probe pods and binaries
# are removed even when a node misses the target.
set -euo pipefail

namespace=${NAMESPACE:-valkey}
nodes=${NODES:-"node1 node2 node3 worker1"}
requests=${REQUESTS:-100000}
warmup=${WARMUP:-5000}
concurrency=${CONCURRENCY:-5}
mode=${MODE:-both}
target=${TARGET:-2ms}
require_target=${REQUIRE_TARGET:-false}
image=${BENCH_IMAGE:-alpine:3.22}
run_id=$(date -u +%Y%m%d%H%M%S)
declare -a pods=()
amd64_binary="/tmp/valkey-live-acceptance-amd64-${run_id}"
arm64_binary="/tmp/valkey-live-acceptance-arm64-${run_id}"

cleanup() {
  for pod in "${pods[@]}"; do
    kubectl -n "$namespace" delete pod "$pod" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  done
  rm -f "$amd64_binary" "$arm64_binary"
}
trap cleanup EXIT INT TERM

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -o "$amd64_binary" ./deploy/k8s/valkey-live-acceptance
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath \
  -o "$arm64_binary" ./deploy/k8s/valkey-live-acceptance

status=0
for node in $nodes; do
  arch=$(kubectl get node "$node" -o jsonpath='{.status.nodeInfo.architecture}')
  if [[ $arch == arm64 ]]; then
    binary=$arm64_binary
  else
    binary=$amd64_binary
  fi
  pod="valkey-p99-${node}-${run_id}"
  pods+=("$pod")

  overrides=$(jq -nc --arg pod "$pod" --arg node "$node" --arg image "$image" '{
    spec:{nodeName:$node,restartPolicy:"Never",containers:[{
      name:$pod,image:$image,command:["sleep","1800"],
      env:[
        {name:"NODE_NAME",valueFrom:{fieldRef:{fieldPath:"spec.nodeName"}}},
        {name:"NODE_IP",valueFrom:{fieldRef:{fieldPath:"status.hostIP"}}},
        {name:"VALKEY_LOCAL_ADDR",value:"valkey-local.valkey.svc.cluster.local:6380"},
        {name:"REDISCLI_AUTH",valueFrom:{secretKeyRef:{name:"valkey-core-secret",key:"valkey-password"}}}
      ],
      volumeMounts:[{name:"tls",mountPath:"/etc/valkey/tls",readOnly:true}],
      resources:{requests:{cpu:"500m",memory:"128Mi"},limits:{cpu:"2",memory:"768Mi"}},
      securityContext:{runAsUser:999,runAsGroup:999,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
    }],volumes:[{name:"tls",secret:{secretName:"valkey-server-tls"}}],
    securityContext:{runAsNonRoot:true,runAsUser:999,runAsGroup:999,seccompProfile:{type:"RuntimeDefault"}}
  }}')

  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never --overrides="$overrides" -- sleep 1800 >/dev/null
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
  kubectl -n "$namespace" cp "$binary" "$pod:/tmp/valkey-live-acceptance"

  args=(
    -node "$node" -mode "$mode" -requests "$requests" -warmup "$warmup"
    -concurrency "$concurrency" -target "$target"
  )
  if [[ $require_target == true ]]; then
    args+=(-require-target)
  fi
  if ! kubectl -n "$namespace" exec "$pod" -- /tmp/valkey-live-acceptance "${args[@]}"; then
    status=1
  fi
  kubectl -n "$namespace" delete pod "$pod" --wait=false >/dev/null
done

exit "$status"
