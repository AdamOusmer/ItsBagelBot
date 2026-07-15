#!/usr/bin/env bash
# Isolated R3 capacity qualification. This script never edits TWITCH_INGRESS,
# Kubernetes manifests, node sysctls, or NATS server configuration.
set -euo pipefail

if [[ ${CONFIRM_R3_SHADOW:-} != R3-120K ]]; then
  cat <<'PLAN'
R3 shadow plan (no actions performed):
  - create isolated memory-backed R3 streams only under R3_SHADOW_*
  - place one temporary publisher pod on node2, node3, and worker1
  - open two publisher connections per node (six total)
  - keep the NATS server on worker1 as a current follower, never leader
  - calibrate async, official atomic, and official Fast-Ingest modes
  - gate 120,000 events/s at a 126,000/s offered load for five minutes
  - soak the 90,000 events/s operating point for fifteen minutes
  - delete every shadow stream and temporary pod on exit

Set CONFIRM_R3_SHADOW=R3-120K to run this controlled live-cluster test.
PLAN
  exit 0
fi

for command in go jq kubectl; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "missing required command: $command" >&2
    exit 1
  fi
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
profile="$script_dir/r3-capacity.json"
namespace=${NAMESPACE:-production}
image=${BENCH_IMAGE:-alpine:3.22}
setup_secret=${NATS_BENCH_SETUP_SECRET:-nats-bench-setup}
run_id=$(date -u +%Y%m%d%H%M%S)
test_started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
binary="/tmp/nats-r3-120k-${run_id}"
results_dir=${R3_RESULTS_DIR:-"/tmp/nats-r3-results-${run_id}"}
if ! mkdir "$results_dir"; then
  echo "R3_RESULTS_DIR must name a new, empty artifact directory: $results_dir" >&2
  exit 1
fi
nodes=()
pods=()
while IFS= read -r node; do
  nodes+=("$node")
  pods+=("nats-r3-${node}-${run_id}")
done < <(jq -r '.nodes[]' "$profile")
if ((${#nodes[@]} != 3)); then
  echo "R3 capacity profile must define exactly three nodes" >&2
  exit 1
fi
control_pod=${pods[1]}

rated_eps=$(jq -er '.rated_eps' "$profile")
ceiling_offered_eps=$(jq -er '.ceiling_offered_eps' "$profile")
operating_eps=$(jq -er '.operating_eps' "$profile")
operating_min_eps=$(jq -er '.operating_min_eps' "$profile")
publishers_per_node=$(jq -er '.publishers_per_node' "$profile")
window=$(jq -er '.window_per_publisher' "$profile")
payload_bytes=$(jq -er '.payload_bytes' "$profile")
payload_variants=$(jq -er '.payload_variants' "$profile")
atomic_inflight=$(jq -er '.atomic_inflight_per_connection' "$profile")
max_p95_ms=$(jq -er '.puback_p95_max | rtrimstr("ms") | tonumber' "$profile")
max_p99_ms=$(jq -er '.puback_p99_max | rtrimstr("ms") | tonumber' "$profile")

calibration_messages=${CALIBRATION_MESSAGES:-$(jq -er '.calibration_messages' "$profile")}
calibration_seconds=${CALIBRATION_MONITOR_SECONDS:-$(jq -er '.calibration_duration | rtrimstr("s") | tonumber' "$profile")}
ceiling_seconds=${CEILING_SECONDS:-$(jq -er '.ceiling_duration | rtrimstr("m") | tonumber * 60' "$profile")}
operating_seconds=${OPERATING_SECONDS:-$(jq -er '.operating_duration | rtrimstr("m") | tonumber * 60' "$profile")}
latency_hz=${LATENCY_SAMPLES_PER_SECOND:-$(jq -er '.latency_samples_per_second' "$profile")}

current_stream=
current_subject=
stream_created=false

setup_exec() {
  kubectl -n "$namespace" exec "$control_pod" -- sh -c \
    'NATS_USER="$NATS_SETUP_USER" NATS_PASSWORD="$NATS_SETUP_PASSWORD" NATS_CA=/etc/nats-ca/ca.pem exec "$@"' \
    sh "$@"
}

delete_current_stream() {
  if [[ $stream_created != true ]]; then
    return 0
  fi
  local attempt cleanup_log="$results_dir/stream-cleanup.log"
  for attempt in 1 2 3 4 5; do
    if setup_exec /tmp/nats-live-acceptance \
      -domain= -replicas=3 -required-peers=3 \
      -stream "$current_stream" -subject "$current_subject" \
      -create-stream=false -cleanup=true -setup-only=true \
      >>"$cleanup_log" 2>&1; then
      stream_created=false
      return 0
    fi
    sleep "$attempt"
  done
  echo "failed to verify deletion of shadow stream $current_stream after five attempts" >&2
  return 1
}

cleanup() {
  delete_current_stream || true
  kubectl -n "$namespace" delete pod "${pods[@]}" \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -f "$binary"
  echo "R3 artifacts retained in $results_dir"
}
trap cleanup EXIT INT TERM

create_pod() {
  local pod=$1 node=$2 overrides
  overrides=$(jq -nc \
    --arg pod "$pod" --arg node "$node" --arg image "$image" --arg setup_secret "$setup_secret" \
    '{spec:{nodeName:$node,restartPolicy:"Never",containers:[{
      name:$pod,image:$image,command:["sleep","7200"],
      env:[
        {name:"NATS_USER",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_USER"}}},
        {name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:"worker-env",key:"NATS_PASSWORD"}}},
        {name:"NATS_SETUP_USER",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_USER"}}},
        {name:"NATS_SETUP_PASSWORD",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_PASSWORD"}}}
      ],
      volumeMounts:[{name:"fleet-ca",mountPath:"/etc/nats-ca",readOnly:true}],
      resources:{requests:{cpu:"500m",memory:"256Mi"},limits:{cpu:"4",memory:"1Gi"}},
      securityContext:{runAsUser:1000,runAsGroup:1000,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
    }],volumes:[{name:"fleet-ca",configMap:{name:"fleet-ca"}}],
    securityContext:{runAsNonRoot:true,runAsUser:1000,runAsGroup:1000,seccompProfile:{type:"RuntimeDefault"}}}}')
  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never \
    --overrides="$overrides" -- sleep 7200 >/dev/null
}

require_nats_topology() {
  local topology
  topology=$(kubectl -n "$namespace" get pods -l app=nats -o json | jq -c '
    [.items[] | {
      server:.metadata.name,
      node:.spec.nodeName,
      ready:([.status.containerStatuses[]? | select(.name == "nats") | .ready] | any)
    }]
  ')
  if [[ $(jq '[.[] | select(.ready)] | length' <<<"$topology") != 3 ]]; then
    echo "R3 shadow requires exactly the three ready NATS servers in the capacity profile: $topology" >&2
    exit 1
  fi
  for node in "${nodes[@]}"; do
    if [[ $(jq --arg node "$node" '[.[] | select(.node == $node and .ready)] | length' <<<"$topology") != 1 ]]; then
      echo "expected exactly one ready NATS server on $node: $topology" >&2
      exit 1
    fi
  done
  preferred_server=$(jq -er '.[] | select(.node == "node3") | .server' <<<"$topology")
  forbidden_server=$(jq -er '.[] | select(.node == "worker1") | .server' <<<"$topology")
  echo "NATS map: $topology"
  echo "preferred leader on node3: $preferred_server; forbidden leader on worker1: $forbidden_server"
}

prepare_trial_stream() {
  local label=$1 topology_file=$2 leader
  local safe_label
  if ! safe_label=$(tr '[:lower:]-' '[:upper:]_' <<<"$label"); then
    return 1
  fi
  current_stream="R3_SHADOW_${run_id}_${safe_label}"
  current_subject="twitch.outgress.bench.r3.${run_id}.${label}"
  # Mark ownership before the request so a response lost after server-side
  # creation still takes the verified cleanup path.
  stream_created=true

  if ! setup_exec /tmp/nats-live-acceptance \
    -domain= -replicas=3 -required-peers=3 \
    -stream "$current_stream" -subject "$current_subject" \
    -setup-only=true -cleanup=false >/dev/null; then
    return 1
  fi
  if ! setup_exec /tmp/nats-live-acceptance \
    -domain= -replicas=3 -required-peers=3 \
    -stream "$current_stream" -subject "$current_subject" \
    -create-stream=false -cleanup=false -topology-only=true \
    -preferred-leader="$preferred_server" -forbidden-leader="$forbidden_server" \
    -topology-duration=0 >"$topology_file"; then
    return 1
  fi
  if ! leader=$(jq -er '.topology.leader' "$topology_file"); then
    return 1
  fi
  leader_url="tls://${leader}.nats-headless.${namespace}.svc.cluster.local:4222"
}

messages_for_node() {
  local total=$1 index=$2 share=$((total / 3))
  if ((index == 2)); then
    echo $((total - 2 * share))
  else
    echo "$share"
  fi
}

target_for_node() {
  local aggregate=$1
  awk -v rate="$aggregate" 'BEGIN { if (rate <= 0) print 0; else printf "%.6f", rate / 3 }'
}

check_nats_logs() {
  local log_file="$results_dir/nats.log"
  kubectl -n "$namespace" logs -l app=nats -c nats --prefix \
    --since-time="$test_started_at" >"$log_file"
  if grep -Eina \
    'IPQ len limit|slow consumer|write deadline( exceeded)?|no quorum|quorum (lost|unavailable)' \
    "$log_file"; then
    echo "NATS emitted a forbidden queue, slow-consumer, write-deadline, or quorum error" >&2
    return 1
  fi
}

summarize_trial() {
  local label=$1 target=$2 minimum=$3 expected_seconds=$4 compatible=$5 topology_file=$6
  shift 6
  local result_files=("$@")
  if ((${#result_files[@]} != ${#nodes[@]})); then
    echo "expected ${#nodes[@]} result files, got ${#result_files[@]}" >&2
    return 1
  fi
  jq -s \
    --slurpfile topology "$topology_file" \
    --arg label "$label" \
    --argjson target "$target" \
    --argjson minimum "$minimum" \
    --argjson expected_ms "$((expected_seconds * 1000))" \
    --argjson max_p95_ms "$max_p95_ms" \
    --argjson max_p99_ms "$max_p99_ms" \
    --argjson compatible "$compatible" '
      [.[].results[0]] as $r |
      ($r | map(.acknowledged) | add) as $acked |
      ($r | map(.started_unix_ms) | min) as $started |
      ($r | map(.finished_unix_ms) | max) as $finished |
      ($finished - $started) as $duration |
      ($acked / ($duration / 1000)) as $rate |
      ($r | map(.errors) | add) as $errors |
      ($r | map(.timeouts) | add) as $timeouts |
      ($r | map(.reconnects) | add) as $reconnects |
      ($r | map(.disconnects) | add) as $disconnects |
      ($r | map(.async_errors) | add) as $async_errors |
      {
        trial:$label,
        mode:$r[0].mode,
        batch_size:($r[0].batch_size // 0),
        fast_outstanding_acks:($r[0].fast_outstanding_acks // 0),
        elixir_compatible:$compatible,
        target_messages_per_second:$target,
        minimum_messages_per_second:$minimum,
        acknowledged:$acked,
        conservative_duration_ms:$duration,
        aggregate_messages_per_second:$rate,
        puback_min_ms:($r | map(.puback_min_ms) | min),
        worst_node_puback_p50_ms:($r | map(.puback_p50_ms) | max),
        worst_node_puback_p95_ms:($r | map(.puback_p95_ms) | max),
        worst_node_puback_p99_ms:($r | map(.puback_p99_ms) | max),
        puback_max_ms:($r | map(.puback_max_ms) | max),
        errors:$errors,
        timeouts:$timeouts,
        reconnects:$reconnects,
        disconnects:$disconnects,
        async_errors:$async_errors,
        topology:$topology[0].topology,
        nodes:$r,
        passed:(
          ($r | all(.passed == true)) and
          ($r | length) == 3 and
          ($r | all(.mode == $r[0].mode)) and
          ($r | all(.started_unix_ms > 0 and .finished_unix_ms > .started_unix_ms)) and
          $errors == 0 and $timeouts == 0 and $reconnects == 0 and
          $disconnects == 0 and $async_errors == 0 and
          ($r | map(.puback_p95_ms) | max) <= $max_p95_ms and
          ($r | map(.puback_p99_ms) | max) <= $max_p99_ms and
          $topology[0].topology.passed == true and
          ($minimum <= 0 or $rate >= $minimum) and
          (if $target <= 0 then
             $duration <= ($expected_ms * 1.10)
           else
             $duration >= ($expected_ms * 0.99) and $duration <= ($expected_ms * 1.10)
           end)
        )
      }
    ' "${result_files[@]}"
}

supervise_trial_processes() {
  local topology_pid=$1
  shift
  local publisher_pids=("$@")
  local publisher_done=()
  local topology_done=false remaining=$((1 + ${#publisher_pids[@]})) failed=0
  local i pid progressed
  for _ in "${publisher_pids[@]}"; do publisher_done+=(false); done

  while ((remaining > 0)); do
    progressed=false
    if [[ $topology_done == false ]] && ! kill -0 "$topology_pid" 2>/dev/null; then
      if ! wait "$topology_pid"; then failed=1; fi
      topology_done=true
      remaining=$((remaining - 1))
      progressed=true
    fi
    for i in "${!publisher_pids[@]}"; do
      pid=${publisher_pids[$i]}
      if [[ ${publisher_done[$i]} == false ]] && ! kill -0 "$pid" 2>/dev/null; then
        if ! wait "$pid"; then failed=1; fi
        publisher_done[$i]=true
        remaining=$((remaining - 1))
        progressed=true
      fi
    done
    if ((failed)); then
      kill "$topology_pid" "${publisher_pids[@]}" >/dev/null 2>&1 || true
      wait "$topology_pid" >/dev/null 2>&1 || true
      for pid in "${publisher_pids[@]}"; do wait "$pid" >/dev/null 2>&1 || true; done
      return 1
    fi
    if [[ $progressed == false ]]; then sleep 0.2; fi
  done
  return 0
}

run_trial() {
  local label=$1 mode=$2 batch_size=$3 fast_outstanding=$4
  local target=$5 total_messages=$6 load_seconds=$7 minimum=$8 compatible=$9
  local topology_file="$results_dir/${label}-topology.json"
  local topology_err="$results_dir/${label}-topology.err"
  local start_ms node_target monitor_seconds latency_samples

  if ! prepare_trial_stream "$label" "$topology_file"; then
    if delete_current_stream; then return 1; else return 2; fi
  fi
  start_ms=$((($(date +%s) + 5) * 1000))
  # Cover the full 110% duration acceptance window, then leave 15 seconds for
  # follower catch-up. A run slow enough to outlive this monitor cannot pass.
  monitor_seconds=$((load_seconds * 11 / 10 + 15))
  latency_samples=$((load_seconds * latency_hz))
  if ((latency_samples < 500)); then latency_samples=500; fi
  node_target=$(target_for_node "$target")

  setup_exec /tmp/nats-live-acceptance \
    -hub-url="$leader_url" -domain= -replicas=3 -required-peers=3 \
    -stream "$current_stream" -subject "$current_subject" \
    -create-stream=false -cleanup=false -topology-only=true \
    -preferred-leader="$preferred_server" -forbidden-leader="$forbidden_server" \
    -start-at-unix-ms="$start_ms" -topology-duration="${monitor_seconds}s" \
    >"$topology_file" 2>"$topology_err" &
  local topology_pid=$!

  echo "trial=$label mode=$mode batch=$batch_size target=$target messages=$total_messages leader=$leader_url"
  local pids=()
  for i in "${!nodes[@]}"; do
    local node_messages
    node_messages=$(messages_for_node "$total_messages" "$i")
    kubectl -n "$namespace" exec "${pods[$i]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
      /tmp/nats-live-acceptance \
      -hub-url="$leader_url" -domain= -replicas=3 -required-peers=3 \
      -stream "$current_stream" -subject "$current_subject" \
      -create-stream=false -cleanup=false \
      -producer-id="${nodes[$i]}" -messages="$node_messages" \
      -publishers="$publishers_per_node" -window="$window" \
      -payload-bytes="$payload_bytes" -payload-variants="$payload_variants" \
      -mode="$mode" -batch-size="$batch_size" -atomic-inflight="$atomic_inflight" \
      -fast-outstanding-acks="$fast_outstanding" -target-rate="$node_target" \
      -start-at-unix-ms="$start_ms" -latency-samples="$latency_samples" \
      -latency-interval=50ms -max-p95="${max_p95_ms}ms" -max-p99="${max_p99_ms}ms" -min-rate=0 \
      >"$results_dir/${label}-${nodes[$i]}.json" \
      2>"$results_dir/${label}-${nodes[$i]}.err" &
    pids+=("$!")
  done

  if ! supervise_trial_processes "$topology_pid" "${pids[@]}"; then
    for file in "$results_dir/${label}"-*.err; do
      sed "s#^#$(basename "$file"): #" "$file" >&2 || true
    done
    if delete_current_stream; then return 1; else return 2; fi
  fi

  local summary_file="$results_dir/summary-${label}.json"
  local result_files=()
  for node in "${nodes[@]}"; do
    result_files+=("$results_dir/${label}-${node}.json")
  done
  if ! summarize_trial \
    "$label" "$target" "$minimum" "$load_seconds" "$compatible" "$topology_file" \
    "${result_files[@]}" >"$summary_file"; then
    if delete_current_stream; then return 1; else return 2; fi
  fi
  if ! jq . "$summary_file"; then
    if delete_current_stream; then return 1; else return 2; fi
  fi
  if ! delete_current_stream; then return 2; fi
  jq -e '.passed' "$summary_file" >/dev/null
}

echo "building static NATS 2.14 acceptance binary (no Docker invocation)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$binary" ./deploy/k8s/nats-live-acceptance

if ! kubectl -n "$namespace" get secret "$setup_secret" >/dev/null 2>&1; then
  echo "missing temporary JetStream setup secret $setup_secret (see README.md)" >&2
  exit 1
fi

require_nats_topology
for i in "${!nodes[@]}"; do
  create_pod "${pods[$i]}" "${nodes[$i]}"
done
for pod in "${pods[@]}"; do
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
  kubectl -n "$namespace" cp "$binary" "$pod:/tmp/nats-live-acceptance"
done

async_label_batch=$(jq -er '.atomic_batch_sizes[1]' "$profile")
default_outstanding=$(jq -er '.fast_outstanding_acks[0]' "$profile")
calibration_specs=("async:${async_label_batch}:${default_outstanding}:true")
while IFS= read -r batch; do
  calibration_specs+=("atomic:${batch}:${default_outstanding}:true")
done < <(jq -r '.atomic_batch_sizes[]' "$profile")
while IFS= read -r flow; do
  while IFS= read -r outstanding; do
    calibration_specs+=("fast:${flow}:${outstanding}:false")
  done < <(jq -r '.fast_outstanding_acks[]' "$profile")
done < <(jq -r '.fast_flows[]' "$profile")

for spec in "${calibration_specs[@]}"; do
  IFS=: read -r mode batch outstanding compatible <<<"$spec"
  label="cal-${mode}-${batch}-${outstanding}"
  if run_trial "$label" "$mode" "$batch" "$outstanding" \
    0 "$calibration_messages" "$calibration_seconds" 0 "$compatible"; then
    :
  else
    trial_status=$?
    if ((trial_status == 2)); then
      echo "calibration $label could not verify stream cleanup; aborting the matrix" >&2
      exit 1
    fi
    echo "calibration $label did not qualify; continuing the matrix" >&2
  fi
done

shopt -s nullglob
calibration_summaries=("$results_dir"/summary-cal-*.json)
if ((${#calibration_summaries[@]} == 0)); then
  echo "no calibration produced a qualifying result" >&2
  exit 1
fi
qualifier=$(jq -s '
  [.[] | select(.elixir_compatible == true and .passed == true)] |
  if length == 0 then null else max_by(.aggregate_messages_per_second) end
' "${calibration_summaries[@]}")
if [[ $qualifier == null ]]; then
  echo "no Elixir-compatible async/atomic calibration passed" >&2
  exit 1
fi
qualifier_mode=$(jq -r '.mode | split("+")[0]' <<<"$qualifier")
qualifier_batch=$(jq -r '.trial | split("-")[2] | tonumber' <<<"$qualifier")
qualifier_outstanding=$default_outstanding
echo "selected Elixir-compatible qualifier: $qualifier_mode batch=$qualifier_batch"

ceiling_messages=$((ceiling_offered_eps * ceiling_seconds))
run_trial "ceiling-${qualifier_mode}-${qualifier_batch}" \
  "$qualifier_mode" "$qualifier_batch" "$qualifier_outstanding" \
  "$ceiling_offered_eps" "$ceiling_messages" "$ceiling_seconds" "$rated_eps" true

operating_messages=$((operating_eps * operating_seconds))
run_trial "operating-${qualifier_mode}-${qualifier_batch}" \
  "$qualifier_mode" "$qualifier_batch" "$qualifier_outstanding" \
  "$operating_eps" "$operating_messages" "$operating_seconds" "$operating_min_eps" true

check_nats_logs
echo "R3 qualification passed: rated=${rated_eps}/s operating=${operating_eps}/s (75%)"
