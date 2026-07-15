#!/usr/bin/env bash
# Runs the WebSocket-only shard benchmark on each production ingress node in a
# temporary, CPU-limited pod. It publishes nothing to NATS and never connects
# to Twitch; the benchmark pod hosts its own Twitch-shaped WebSocket endpoint.
set -euo pipefail

namespace=${NAMESPACE:-production}
image=${BENCH_IMAGE:-docker.io/library/elixir:1.17-otp-27}
events=${EVENTS:-500000}
warmup=${WARMUP:-25000}
samples=${SAMPLES:-3}
nodes_string=${NODES:-"node2 node3 worker1"}
run_id=$(date -u +%Y%m%d%H%M%S)
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ingress_dir=$(cd "$script_dir/.." && pwd)
pods=()

read -r -a nodes <<<"$nodes_string"

cleanup() {
  if ((${#pods[@]} > 0)); then
    kubectl -n "$namespace" delete pod "${pods[@]}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

create_pod() {
  local pod=$1 node=$2
  local manifest

  manifest=$(jq -nc \
    --arg pod "$pod" \
    --arg node "$node" \
    --arg image "$image" \
    --arg run "$run_id" \
    '{
      apiVersion:"v1",
      kind:"Pod",
      metadata:{
        name:$pod,
        labels:{app:"ingress-websocket-bench",run:$run}
      },
      spec:{
        nodeName:$node,
        enableServiceLinks:false,
        restartPolicy:"Never",
        containers:[{
          name:"bench",
          image:$image,
          command:["sleep","1800"],
          resources:{
            requests:{cpu:"500m",memory:"512Mi"},
            limits:{cpu:"2",memory:"1Gi"}
          },
          securityContext:{
            allowPrivilegeEscalation:false,
            capabilities:{drop:["ALL"]}
          }
        }]
      }
    }')

  jq -e . <<<"$manifest" | kubectl -n "$namespace" create -f - >/dev/null
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=180s >/dev/null
}

copy_source() {
  local pod=$1

  kubectl -n "$namespace" exec "$pod" -- mkdir -p /bench/lib /bench/config /bench/bench
  kubectl -n "$namespace" cp --no-preserve "$ingress_dir/mix.exs" "$pod:/bench/mix.exs"
  kubectl -n "$namespace" cp --no-preserve "$ingress_dir/mix.lock" "$pod:/bench/mix.lock"
  kubectl -n "$namespace" cp --no-preserve "$ingress_dir/lib/." "$pod:/bench/lib"
  kubectl -n "$namespace" cp --no-preserve "$ingress_dir/config/." "$pod:/bench/config"
  kubectl -n "$namespace" cp --no-preserve "$script_dir/websocket_shard.exs" "$pod:/bench/bench/websocket_shard.exs"
}

prepare() {
  local pod=$1

  kubectl -n "$namespace" exec "$pod" -- sh -c \
    "openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
      -keyout /tmp/ingress-ws-bench-ca.key -out /tmp/ingress-ws-bench-ca.crt \
      -subj /CN=ingress-ws-bench-ca -addext basicConstraints=critical,CA:TRUE >/dev/null 2>&1 && \
     openssl req -new -newkey rsa:2048 -nodes \
      -keyout /tmp/ingress-ws-bench.key -out /tmp/ingress-ws-bench.csr \
      -subj /CN=127.0.0.1 -addext subjectAltName=IP:127.0.0.1 >/dev/null 2>&1 && \
     openssl x509 -req -in /tmp/ingress-ws-bench.csr \
      -CA /tmp/ingress-ws-bench-ca.crt -CAkey /tmp/ingress-ws-bench-ca.key \
      -CAcreateserial -days 1 -copy_extensions copy \
      -out /tmp/ingress-ws-bench.crt >/dev/null 2>&1"

  kubectl -n "$namespace" exec "$pod" -- env MIX_ENV=test sh -c \
    'cd /bench && mix local.hex --force && mix local.rebar --force && mix deps.get && mix deps.compile'
}

run_sample() {
  local pod=$1 node=$2 sample=$3

  echo "websocket shard benchmark node=$node sample=$sample/$samples"
  kubectl -n "$namespace" exec "$pod" -- env \
    'ERL_FLAGS=+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none' \
    MIX_ENV=test \
    INGRESS_WS_BENCH_TLS=true \
    INGRESS_WS_BENCH_CAFILE=/tmp/ingress-ws-bench-ca.crt \
    INGRESS_WS_BENCH_CERTFILE=/tmp/ingress-ws-bench.crt \
    INGRESS_WS_BENCH_KEYFILE=/tmp/ingress-ws-bench.key \
    INGRESS_WS_BENCH_EVENTS="$events" \
    INGRESS_WS_BENCH_WARMUP="$warmup" \
    sh -c 'cd /bench && mix run --no-start bench/websocket_shard.exs'
}

for node in "${nodes[@]}"; do
  pod="ingress-ws-bench-${node}-${run_id}"
  pods+=("$pod")
  create_pod "$pod" "$node"
  copy_source "$pod"
  prepare "$pod"

  for sample in $(seq 1 "$samples"); do
    run_sample "$pod" "$node" "$sample"
  done

  kubectl -n "$namespace" delete pod "$pod" --wait=true >/dev/null
done
