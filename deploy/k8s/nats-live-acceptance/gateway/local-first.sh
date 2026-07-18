#!/usr/bin/env bash
# Opt-in, isolated proof of NATS gateway local-first queue behavior.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
namespace=${NAMESPACE:-production}
confirmation=${CONFIRM_NATS_GATEWAY_ACCEPTANCE:-}
correctness_requests=${CORRECTNESS_REQUESTS:-500}
latency_requests=${LATENCY_REQUESTS:-5000}
latency_clients=${LATENCY_CLIENTS:-4}
rpc_p99_max_ms=${RPC_P99_MAX_MS:-8}
queue_group=${QUEUE_GROUP:-RPC_GATEWAY_ACCEPTANCE}
run_id=$(date -u +%Y%m%d%H%M%S)
results_file=${RESULTS_FILE:-/tmp/nats-gateway-local-first-${run_id}.json}
phase_results=""
owns_resources=false

servers=(
  nats-gw-accept-core-0
  nats-gw-accept-core-1
  nats-gw-accept-worker1
  nats-gw-accept-node1
)
clients=(
  nats-gw-accept-client-node2
  nats-gw-accept-client-node3
  nats-gw-accept-client-worker1
  nats-gw-accept-client-node1
)

if [[ "$confirmation" != "LOCAL-FIRST-RPC" ]]; then
  cat <<'EOF'
Plan only; no actions performed.

This guarded lane creates four temporary, core-NATS-only servers and four
nats-box clients. node2+node3 form rpc-core; worker1 and node1 are independent
edge clusters joined through mutually verified native-TLS gateways. A NetworkPolicy
prevents every acceptance pod from reaching the production NATS hub or leaf tier.

Run only in a controlled window:
  CONFIRM_NATS_GATEWAY_ACCEPTANCE=LOCAL-FIRST-RPC \
    deploy/k8s/nats-live-acceptance/gateway/local-first.sh
EOF
  exit 0
fi

fail() {
  echo "gateway acceptance failed: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

cleanup() {
  if [[ -n "$phase_results" ]]; then rm -f "$phase_results"; fi
  if $owns_resources; then
    delete_lane >/dev/null 2>&1 || echo "warning: bounded gateway acceptance cleanup did not complete" >&2
  fi
}
phase_results=$(mktemp /tmp/nats-gateway-local-first-phases.XXXXXX)

delete_lane() {
  local secret_json
  kubectl delete -k "$script_dir" --ignore-not-found --wait=true --timeout=120s >/dev/null || return 1

  if ! secret_json=$(kubectl -n "$namespace" get secret nats-gateway-acceptance-tls -o json 2>/dev/null); then
    return 0
  fi
  jq -e '
    .metadata.name == "nats-gateway-acceptance-tls" and
    .metadata.labels["itsbagelbot.dev/acceptance-owner"] == "local-first-rpc" and
    .metadata.labels["app.kubernetes.io/name"] == "nats-gateway-acceptance" and
    .metadata.annotations["itsbagelbot.dev/acceptance-certificate"] == "nats-gateway-acceptance-tls"
  ' <<<"$secret_json" >/dev/null || {
    echo "refusing to delete nats-gateway-acceptance-tls: ownership metadata does not match this lane" >&2
    return 1
  }
  kubectl -n "$namespace" delete secret nats-gateway-acceptance-tls \
    --wait=true --timeout=30s >/dev/null
}
trap cleanup EXIT INT TERM

monitor() {
  local pod=$1 endpoint=$2
  kubectl -n "$namespace" exec "$pod" -c nats -- \
    wget -qO- "http://127.0.0.1:8222/$endpoint"
}

production_nats_baseline() {
  kubectl -n "$namespace" get pods -l 'app in (nats,nats-leaf)' -o json |
    jq -Sc '[.items[] | {
      name:.metadata.name,
      uid:.metadata.uid,
      node:.spec.nodeName,
      restarts:([.status.containerStatuses[]?.restartCount] | add // 0)
    }] | sort_by(.name)'
}

assert_no_existing_lane() {
  local names=(
    certificate/nats-gateway-acceptance-tls
    secret/nats-gateway-acceptance-tls
    networkpolicy/nats-gateway-acceptance-isolation
    service/nats-gw-accept-core-0
    service/nats-gw-accept-core-1
    service/nats-gw-accept-worker1
    service/nats-gw-accept-node1
    pod/nats-gw-accept-core-0
    pod/nats-gw-accept-core-1
    pod/nats-gw-accept-worker1
    pod/nats-gw-accept-node1
    pod/nats-gw-accept-client-node2
    pod/nats-gw-accept-client-node3
    pod/nats-gw-accept-client-worker1
    pod/nats-gw-accept-client-node1
  )
  local resource
  for resource in "${names[@]}"; do
    if kubectl -n "$namespace" get "$resource" >/dev/null 2>&1; then
      fail "$resource already exists; refusing to adopt or delete it"
    fi
  done
  if kubectl -n "$namespace" get configmap \
    -l app.kubernetes.io/name=nats-gateway-acceptance \
    -o json | jq -e '.items | length > 0' >/dev/null; then
    fail "an acceptance ConfigMap already exists; clean up its owning run first"
  fi
}

assert_prerequisites() {
  [[ "$namespace" == "production" ]] || fail "NAMESPACE must be production because credentials and fleet PKI are namespace-scoped"
  [[ "$correctness_requests" =~ ^[1-9][0-9]*$ ]] || fail "CORRECTNESS_REQUESTS must be positive"
  [[ "$latency_requests" =~ ^[1-9][0-9]*$ ]] || fail "LATENCY_REQUESTS must be positive"
  [[ "$latency_clients" =~ ^[1-9][0-9]*$ ]] || fail "LATENCY_CLIENTS must be positive"
  awk -v gate="$rpc_p99_max_ms" 'BEGIN { exit !(gate > 0 && gate <= 8) }' || \
    fail "RPC_P99_MAX_MS must be positive and cannot relax the 8ms production ceiling"

  local priority_value preemption
  priority_value=$(kubectl get priorityclass nats-r3-bench-nonpreempting -o jsonpath='{.value}')
  preemption=$(kubectl get priorityclass nats-r3-bench-nonpreempting -o jsonpath='{.preemptionPolicy}')
  [[ "$priority_value" =~ ^-?[0-9]+$ ]] || fail "invalid benchmark PriorityClass value"
  (( priority_value <= 0 )) || fail "benchmark PriorityClass must not outrank production"
  [[ "$preemption" == "Never" ]] || fail "benchmark PriorityClass must never preempt production"

  local node ready
  for node in node1 node2 node3 worker1; do
    ready=$(kubectl get node "$node" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
    [[ "$ready" == "True" ]] || fail "$node is not Ready"
  done
  kubectl -n "$namespace" get secret nats-auth-env users-env console-admin-env >/dev/null
  kubectl -n "$namespace" get configmap nats-leaf-config fleet-ca >/dev/null
  kubectl get clusterissuer fleet-ca-issuer >/dev/null

  local rendered
  rendered=$(kubectl kustomize "$script_dir")
  if grep -Eq '(^|[[:space:]])(jetstream|leafnodes)[[:space:]]*[{:]' <<<"$rendered"; then
    fail "isolated topology must not enable JetStream or leaf nodes"
  fi
  grep -q 'kind: NetworkPolicy' <<<"$rendered" || fail "isolation NetworkPolicy is missing"
}

wait_for_connection() {
  local server=$1 name=$2 expected=${3:-present}
  local count=0
  for _ in $(seq 1 100); do
    count=$(monitor "$server" 'connz?subs=detail' |
      jq --arg name "$name" '[.connections[]? | select((.name // "") | startswith($name))] | length')
    if [[ "$expected" == "present" && "$count" -gt 0 ]]; then
      monitor "$server" 'connz?subs=detail' |
        jq -e --arg name "$name" '[.connections[]? | select(((.name // "") | startswith($name)) and (.tls_version // "") != "")] | length > 0' >/dev/null ||
        fail "$name connected without verified client TLS"
      return 0
    fi
    if [[ "$expected" == "absent" && "$count" -eq 0 ]]; then
      return 0
    fi
    sleep 0.1
  done
  fail "$name did not become $expected on $server"
}

start_tagged_responder() {
  local pod=$1 server=$2 subject=$3 tag=$4 id=$5
  kubectl -n "$namespace" exec "$pod" -- sh -c '
    nats --no-context --connection-name "$5" \
      --server "$LOCAL_NATS_URL" --user "$RESPONSE_USER" --password "$RESPONSE_PASSWORD" \
      reply --queue "$4" --count "$3" "$1" "$2" >"/tmp/$5.log" 2>&1 &
    echo $! >"/tmp/$5.pid"
  ' sh "$subject" "$tag" "$((correctness_requests * 4))" "$queue_group" "$id" >/dev/null
  wait_for_connection "$server" "$id"
}

start_bench_responder() {
  local pod=$1 server=$2 subject=$3 id=$4
  kubectl -n "$namespace" exec "$pod" -- sh -c '
    nats --no-context --connection-name "$4" \
      --server "$LOCAL_NATS_URL" --user "$RESPONSE_USER" --password "$RESPONSE_PASSWORD" \
      bench service serve --msgs "$2" --clients "$3" --no-progress "$1" >"/tmp/$4.log" 2>&1 &
    echo $! >"/tmp/$4.pid"
  ' sh "$subject" "$latency_requests" "$latency_clients" "$id" >/dev/null
  wait_for_connection "$server" "$id"
}

stop_responder() {
  local pod=$1 server=$2 id=$3
  kubectl -n "$namespace" exec "$pod" -- sh -c '
    pid=$(cat "/tmp/$1.pid" 2>/dev/null || true)
    if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; fi
    rm -f "/tmp/$1.pid"
  ' sh "$id" >/dev/null
  wait_for_connection "$server" "$id" absent
}

assert_responses() {
  local requester=$1 subject=$2 expected=$3 label=$4
  local output total matching
  output=$(kubectl -n "$namespace" exec "$requester" -- sh -c '
    nats --no-context --server "$LOCAL_NATS_URL" \
      --user "$REQUEST_USER" --password "$REQUEST_PASSWORD" \
      request --raw --count "$2" "$1" acceptance
  ' sh "$subject" "$correctness_requests")
  total=$(awk 'NF {count++} END {print count+0}' <<<"$output")
  matching=$(awk -v expected="$expected" '$0 == expected {count++} END {print count+0}' <<<"$output")
  [[ "$total" -eq "$correctness_requests" ]] || fail "$label returned $total/$correctness_requests replies"
  [[ "$matching" -eq "$correctness_requests" ]] || fail "$label routed $((total - matching)) requests to a non-$expected responder"
  jq -nc --arg phase "$label" --arg responder "$expected" --argjson requests "$total" \
    '{phase:$phase,expected_responder:$responder,requests:$requests,passed:true}'
}

duration_ms() {
  local value=$1
  awk -v value="$value" 'BEGIN {
    if (value ~ /ns$/) { sub(/ns$/, "", value); print value / 1000000; exit }
    if (value ~ /(µs|us)$/) { sub(/(µs|us)$/, "", value); print value / 1000; exit }
    if (value ~ /ms$/) { sub(/ms$/, "", value); print value; exit }
    if (value ~ /s$/) { sub(/s$/, "", value); print value * 1000; exit }
    exit 1
  }'
}

measure_p99() {
  local requester=$1 subject=$2 label=$3
  local output raw_p99 p99_ms
  output=$(kubectl -n "$namespace" exec "$requester" -- sh -c '
    nats --no-context --server "$LOCAL_NATS_URL" \
      --user "$REQUEST_USER" --password "$REQUEST_PASSWORD" \
      bench service request --msgs "$2" --clients "$3" --no-progress "$1"
  ' sh "$subject" "$latency_requests" "$latency_clients")
  raw_p99=$(awk '$1 == "99:" {print $2; exit}' <<<"$output")
  [[ -n "$raw_p99" ]] || fail "$label did not report a p99 latency"
  p99_ms=$(duration_ms "$raw_p99") || fail "$label reported an unknown p99 duration: $raw_p99"
  awk -v actual="$p99_ms" -v gate="$rpc_p99_max_ms" 'BEGIN {exit !(actual <= gate)}' ||
    fail "$label p99 ${p99_ms}ms exceeds the ${rpc_p99_max_ms}ms ceiling"
  jq -nc --arg phase "$label" --argjson p99_ms "$p99_ms" --argjson gate_ms "$rpc_p99_max_ms" \
    --argjson requests "$latency_requests" \
    '{phase:$phase,requests:$requests,p99_ms:$p99_ms,gate_ms:$gate_ms,passed:true}'
}

run_scenario() {
  local name=$1 requester=$2 local_client=$3 local_server=$4 remote_client=$5 remote_server=$6
  local subject="bagel.rpc.admin.user.bench.gatewaylocal.${name}.${run_id}"
  local local_tag="local-${name}" remote_tag="remote-${name}"
  local local_id="gw-local-${name}" remote_id="gw-remote-${name}"
  local bench_local_id="gw-bench-local-${name}" bench_remote_id="gw-bench-remote-${name}"

  start_tagged_responder "$remote_client" "$remote_server" "$subject" "$remote_tag" "$remote_id"
  start_tagged_responder "$local_client" "$local_server" "$subject" "$local_tag" "$local_id"
  sleep 1
  assert_responses "$requester" "$subject" "$local_tag" "${name}-local-priority"

  stop_responder "$local_client" "$local_server" "$local_id"
  sleep 1
  assert_responses "$requester" "$subject" "$remote_tag" "${name}-remote-fallback"
  stop_responder "$remote_client" "$remote_server" "$remote_id"

  start_bench_responder "$local_client" "$local_server" "$subject" "$bench_local_id"
  measure_p99 "$requester" "$subject" "${name}-local-latency"
  wait_for_connection "$local_server" "$bench_local_id" absent

  start_bench_responder "$remote_client" "$remote_server" "$subject" "$bench_remote_id"
  measure_p99 "$requester" "$subject" "${name}-remote-fallback-latency"
  wait_for_connection "$remote_server" "$bench_remote_id" absent
}

for command in kubectl jq awk grep; do require_command "$command"; done
assert_prerequisites
assert_no_existing_lane
baseline_before=$(production_nats_baseline)

owns_resources=true
kubectl apply -k "$script_dir" >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready \
  certificate/nats-gateway-acceptance-tls --timeout=90s >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready \
  $(printf 'pod/%s ' "${servers[@]}" "${clients[@]}") --timeout=180s >/dev/null

expected_placements='{"nats-gw-accept-core-0":"node2","nats-gw-accept-core-1":"node3","nats-gw-accept-worker1":"worker1","nats-gw-accept-node1":"node1","nats-gw-accept-client-node2":"node2","nats-gw-accept-client-node3":"node3","nats-gw-accept-client-worker1":"worker1","nats-gw-accept-client-node1":"node1"}'
for pod in "${servers[@]}" "${clients[@]}"; do
  actual_node=$(kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.spec.nodeName}')
  expected_node=$(jq -r --arg pod "$pod" '.[$pod]' <<<"$expected_placements")
  [[ "$actual_node" == "$expected_node" ]] || fail "$pod landed on $actual_node instead of $expected_node"
done

for server in "${servers[@]}"; do
  if jsz=$(monitor "$server" jsz 2>/dev/null); then
    jq -e '(.error // "") | test("not enabled"; "i")' <<<"$jsz" >/dev/null ||
      fail "$server exposed an enabled JetStream plane"
  fi
done

run_scenario core nats-gw-accept-client-node2 nats-gw-accept-client-node3 nats-gw-accept-core-1 nats-gw-accept-client-node1 nats-gw-accept-node1 >>"$phase_results"
run_scenario worker1 nats-gw-accept-client-worker1 nats-gw-accept-client-worker1 nats-gw-accept-worker1 nats-gw-accept-client-node2 nats-gw-accept-core-0 >>"$phase_results"
run_scenario node1 nats-gw-accept-client-node1 nats-gw-accept-client-node1 nats-gw-accept-node1 nats-gw-accept-client-node3 nats-gw-accept-core-1 >>"$phase_results"

jq -s --arg topology "node2+node3 core; worker1 edge; node1 edge" \
  --argjson p99_gate_ms "$rpc_p99_max_ms" \
  '{topology:$topology,rpc_p99_gate_ms:$p99_gate_ms,jetstream:false,hub_dependency:false,phases:.,passed:all(.[];.passed)}' \
  "$phase_results" | tee "$results_file"

delete_lane || fail "bounded cleanup did not remove the owned acceptance resources"
owns_resources=false
baseline_after=$(production_nats_baseline)
[[ "$baseline_after" == "$baseline_before" ]] || fail "production NATS/leaf pod identity or restart count changed during the isolated lane"

echo "gateway acceptance passed; results: $results_file"
