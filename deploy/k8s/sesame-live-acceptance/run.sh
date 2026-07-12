#!/usr/bin/env bash
# Isolated production Sesame capacity test. The temporary stream, consumer,
# The temporary stream, consumer, pod and local binary are self-cleaning.
set -euo pipefail

namespace=${NAMESPACE:-production}
node=${NODE:-node3}
messages=${MESSAGES:-50000}
channels=${CHANNELS:-1}
min_routines=${MIN_ROUTINES:-100}
max_routines=${MAX_ROUTINES:-100}
min_consumers=${MIN_CONSUMERS:-1}
max_consumers=${MAX_CONSUMERS:-4}
output_mode=${OUTPUT_MODE:-nats}
nats_url=${BENCH_NATS_URL:-tls://nats:4222}
output_nats_url=${BENCH_OUTPUT_NATS_URL:-$nats_url}
image=${BENCH_IMAGE:-alpine:3.22}
run_id=$(date -u +%Y%m%d%H%M%S)
pod="sesame-bench-${node}-${run_id}"
binary="/tmp/sesame-live-acceptance-${run_id}"
result_file=$(mktemp)
samples_file=$(mktemp)

cleanup() {
  kubectl -n "$namespace" delete pod "$pod" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -f "$binary" "$result_file" "$samples_file"
}
trap cleanup EXIT INT TERM

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$binary" ./deploy/k8s/sesame-live-acceptance

overrides=$(jq -nc --arg pod "$pod" --arg node "$node" --arg image "$image" '{
  spec:{nodeName:$node,restartPolicy:"Never",containers:[{
    name:$pod,image:$image,command:["sleep","1800"],
    envFrom:[{secretRef:{name:"sesame-env"}}],
    env:[
      {name:"NODE_NAME",valueFrom:{fieldRef:{fieldPath:"spec.nodeName"}}},
      {name:"GOMEMLIMIT",value:"512MiB"},
      {name:"NATS_CA",value:"/etc/nats-ca/ca.pem"},
      {name:"SSL_CERT_FILE",value:"/etc/nats-ca/ca.pem"},
      {name:"NATS_SUB_USER",valueFrom:{secretKeyRef:{name:"outgress-env",key:"NATS_USER"}}},
      {name:"NATS_SUB_PASSWORD",valueFrom:{secretKeyRef:{name:"outgress-env",key:"NATS_PASSWORD"}}}
    ],
    volumeMounts:[{name:"fleet-ca",mountPath:"/etc/nats-ca",readOnly:true}],
    resources:{requests:{cpu:"200m",memory:"128Mi"},limits:{cpu:"2",memory:"768Mi"}},
    securityContext:{runAsUser:1000,runAsGroup:1000,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
  }],volumes:[{name:"fleet-ca",configMap:{name:"fleet-ca"}}],
  securityContext:{runAsNonRoot:true,runAsUser:1000,runAsGroup:1000,seccompProfile:{type:"RuntimeDefault"}}}
}')

kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never --overrides="$overrides" -- sleep 1800 >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
kubectl -n "$namespace" cp "$binary" "$pod:/tmp/sesame-live-acceptance"

kubectl -n "$namespace" exec "$pod" -- /tmp/sesame-live-acceptance \
  -url "$nats_url" -output-url "$output_nats_url" \
  -messages "$messages" -channels "$channels" \
  -output "$output_mode" \
  -min-routines "$min_routines" -max-routines "$max_routines" \
  -min-consumers "$min_consumers" -max-consumers "$max_consumers" >"$result_file" &
runner=$!

while kill -0 "$runner" 2>/dev/null; do
  kubectl -n "$namespace" top pod "$pod" --containers --no-headers 2>/dev/null >>"$samples_file" || true
  sleep 1
done
set +e
wait "$runner"
status=$?
set -e
cat "$result_file"
if ((status != 0)); then
  exit "$status"
fi
awk '
  function cpu_m(v) { if (v ~ /m$/) { sub(/m$/, "", v); return v+0 } sub(/n$/, "", v); return (v+0)/1000000 }
  function mem_mi(v) {
    if (v ~ /Gi$/) { sub(/Gi$/, "", v); return (v+0)*1024 }
    if (v ~ /Ki$/) { sub(/Ki$/, "", v); return (v+0)/1024 }
    sub(/Mi$/, "", v); return v+0
  }
  { c=cpu_m($3); m=mem_mi($4); if(c>mc)mc=c; if(m>mm)mm=m }
  END { printf "{\"peak_cpu_millicores\":%.0f,\"peak_memory_mib\":%.1f}\n", mc, mm }
' "$samples_file"
