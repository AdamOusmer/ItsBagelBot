#!/usr/bin/env bash
# Isolated R3 capacity qualification. This script never edits TWITCH_INGRESS,
# Kubernetes manifests, node sysctls, or NATS server configuration.
set -euo pipefail

if [[ ${CONFIRM_R3_SHADOW:-} != R3-120K ]]; then
  cat <<'PLAN'
R3 shadow plan (no actions performed):
  - recreate only the ACL-scoped R3_SHADOW_BENCH stream with a per-trial subject
  - place one temporary publisher pod on node2, node3, and worker1
  - open two publisher connections per node (six total)
  - keep the NATS server on worker1 as a current follower, never leader
  - calibrate async, official atomic, and official Fast-Ingest modes
  - cap calibration at 120,000/s; every generator pod is limited to one CPU
	  - abort if any production Deployment loses availability or a NATS pod
	    becomes unready, restarts, reconnects, or changes identity
	  - R3_SLI_ONLY=true measures the three node-local RPC/Valkey lanes without
	    creating a stream or publisher pod
	  - gate 120,000 events/s at a 126,000/s offered load for five minutes
  - publish node-locally by default; R3_PUBLISH_TARGET=preferred is comparison-only
  - soak the 90,000 events/s operating point for thirty minutes
  - delete the owned shadow stream and every temporary pod on exit

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
valkey_namespace=${VALKEY_NAMESPACE:-valkey}
image=${BENCH_IMAGE:-alpine:3.22}
setup_secret=${NATS_BENCH_SETUP_SECRET:-nats-bench-setup}
publisher_secret=${NATS_BENCH_PUBLISHER_SECRET:-sesame-env}
admin_rpc_secret=${NATS_BENCH_ADMIN_RPC_SECRET:-console-admin-env}
priority_class=${R3_BENCH_PRIORITY_CLASS:-nats-r3-bench-nonpreempting}
shadow_stream=$(jq -er '.stream' "$profile")
run_id=$(date -u +%Y%m%d%H%M%S)-$(printf '%05d' "$RANDOM")
test_started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
binary="/tmp/nats-r3-120k-${run_id}"
results_dir=${R3_RESULTS_DIR:-"/tmp/nats-r3-results-${run_id}"}
if ! mkdir "$results_dir"; then
  echo "R3_RESULTS_DIR must name a new, empty artifact directory: $results_dir" >&2
  exit 1
fi
nodes=()
pods=()
sli_pods=()
while IFS= read -r node; do
  nodes+=("$node")
  pods+=("nats-r3-${node}-${run_id}")
  sli_pods+=("nats-r3-sli-${node}-${run_id}")
done < <(jq -r '.nodes[]' "$profile")
if ((${#nodes[@]} != 3)); then
  echo "R3 capacity profile must define exactly three nodes" >&2
  exit 1
fi
control_pod="nats-r3-control-node3-${run_id}"
all_pods=("$control_pod" "${sli_pods[@]}" "${pods[@]}")

rated_eps=$(jq -er '.rated_eps' "$profile")
ceiling_offered_eps=$(jq -er '.ceiling_offered_eps' "$profile")
operating_eps=$(jq -er '.operating_eps' "$profile")
operating_min_eps=$(jq -er '.operating_min_eps' "$profile")
publishers_per_node=$(jq -er '.publishers_per_node' "$profile")
window=$(jq -er '.window_per_publisher' "$profile")
payload_bytes=$(jq -er '.payload_bytes' "$profile")
payload_variants=$(jq -er '.payload_variants' "$profile")
atomic_inflight=$(jq -er '.atomic_inflight_per_connection' "$profile")
max_p95_ms=${R3_PUBACK_P95_MAX_MS:-$(jq -er '.puback_p95_max | rtrimstr("ms") | tonumber' "$profile")}
normal_load_eps=$(jq -er '.normal_load_eps' "$profile")
normal_load_max_p99_ms=${R3_NORMAL_LOAD_PUBACK_P99_MAX_MS:-${R3_PUBACK_P99_MAX_MS:-$(jq -er '.normal_load_puback_p99_max | rtrimstr("ms") | tonumber' "$profile")}}
loaded_max_p99_ms=${R3_LOADED_PUBACK_P99_MAX_MS:-${R3_PUBACK_P99_MAX_MS:-$(jq -er '.loaded_puback_p99_max | rtrimstr("ms") | tonumber' "$profile")}}
max_broker_cpu_pct=${R3_BROKER_CPU_MAX_PCT:-$(jq -er '.broker_cpu_max_pct' "$profile")}
broker_cpu_limit_cores=$(jq -er '.broker_cpu_limit_cores' "$profile")
max_ack_gap=$(jq -er '.max_ack_gap' "$profile")
publish_target=${R3_PUBLISH_TARGET:-local}

calibration_messages=${CALIBRATION_MESSAGES:-$(jq -er '.calibration_messages' "$profile")}
calibration_target_eps=${CALIBRATION_TARGET_EPS:-$(jq -er '.calibration_target_eps' "$profile")}
calibration_seconds=${CALIBRATION_MONITOR_SECONDS:-$(jq -er '.calibration_duration | rtrimstr("s") | tonumber' "$profile")}
ceiling_seconds=${CEILING_SECONDS:-$(jq -er '.ceiling_duration | rtrimstr("m") | tonumber * 60' "$profile")}
operating_seconds=${OPERATING_SECONDS:-$(jq -er '.operating_duration | rtrimstr("m") | tonumber * 60' "$profile")}
latency_hz=${LATENCY_SAMPLES_PER_SECOND:-$(jq -er '.latency_samples_per_second' "$profile")}
consumer_pending_growth_max=${PRODUCTION_CONSUMER_PENDING_GROWTH_MAX:-1000}
consumer_ack_pending_growth_max=${PRODUCTION_CONSUMER_ACK_PENDING_GROWTH_MAX:-1000}
deployment_query_timeout=${DEPLOYMENT_QUERY_TIMEOUT:-10s}
broker_query_timeout=${BROKER_QUERY_TIMEOUT:-5s}
health_poll_seconds=${HEALTH_POLL_SECONDS:-5}
health_freshness_seconds=${HEALTH_FRESHNESS_SECONDS:-120}
meta_cooldown_seconds=${META_COOLDOWN_SECONDS:-20}
canary_seconds=${R3_CANARY_SECONDS:-60}
canary_rates=${R3_CANARY_RATES:-"12000 30000 60000 90000"}
sli_duration=${R3_SLI_DURATION:-3h}
sli_interval=${R3_SLI_INTERVAL:-5s}
sli_max_rtt=${R3_SLI_MAX_RTT:-250ms}
sli_ingress_max_rtt=${R3_SLI_INGRESS_MAX_RTT:-500ms}
sli_rpc_p99_max=${R3_SLI_RPC_P99_MAX:-$(jq -er '.rpc_p99_max' "$profile")}
sli_rpc_p99_min_samples=${R3_SLI_RPC_P99_MIN_SAMPLES:-$(jq -er '.rpc_p99_min_samples' "$profile")}
sli_only=${R3_SLI_ONLY:-false}
quick_throughput=${R3_QUICK_THROUGHPUT:-false}
quick_seconds=${R3_QUICK_SECONDS:-30}
fast_sustain_only=${R3_FAST_SUSTAIN_ONLY:-false}
fast_sustain_eps=${R3_FAST_SUSTAIN_EPS:-90000}
fast_sustain_min_eps=${R3_FAST_SUSTAIN_MIN_EPS:-89100}
fast_sustain_seconds=${R3_FAST_SUSTAIN_SECONDS:-300}
fast_sustain_batch=${R3_FAST_SUSTAIN_BATCH:-1000}
fast_sustain_outstanding=${R3_FAST_SUSTAIN_OUTSTANDING:-8}
topology_unhealthy_grace=${R3_TOPOLOGY_UNHEALTHY_GRACE:-0s}
pod_lifetime_seconds=
canary_rate_values=()

current_stream=
current_subject=
stream_created=false
cleanup_started=false
cleanup_status=0
active_pids=()
control_pod_created=false
sli_pods_created=false
publisher_pods_created=false
health_baseline_file="$results_dir/cluster-health-baseline.json"
health_monitor_file="$results_dir/production-health.jsonl"
health_monitor_stop="$results_dir/production-health.stop"
health_monitor_ready="$results_dir/production-health.ready"
health_monitor_pid=
health_monitor_started=false
nats_topology=
sli_monitor_files=()
sli_monitor_errs=()
sli_monitor_ready_files=()
sli_monitor_pids=()
sli_monitors_started=false
for node in "${nodes[@]}"; do
	sli_monitor_files+=("$results_dir/live-sli-${node}.jsonl")
	sli_monitor_errs+=("$results_dir/live-sli-${node}.err")
	sli_monitor_ready_files+=("$results_dir/live-sli-${node}.ready")
done

positive_integer() {
	[[ $1 =~ ^[1-9][0-9]*$ ]]
}

case $publish_target in
	local|preferred) ;;
	*)
		echo "R3_PUBLISH_TARGET must be local or preferred" >&2
		exit 1
		;;
esac

duration_seconds() {
	local remaining=$1 total=0 amount unit
	while [[ $remaining =~ ^([0-9]+)(h|m|s)(.*)$ ]]; do
		amount=${BASH_REMATCH[1]}
		unit=${BASH_REMATCH[2]}
		remaining=${BASH_REMATCH[3]}
		case $unit in
		h) total=$((total + 10#$amount * 3600)) ;;
		m) total=$((total + 10#$amount * 60)) ;;
		s) total=$((total + 10#$amount)) ;;
		esac
	done
	if [[ -n $remaining ]] || ((total <= 0)); then
		return 1
	fi
	printf '%d\n' "$total"
}

configure_run_lifetime_and_canaries() {
	local value sli_seconds atomic_modes fast_flows fast_outstanding i
	local calibration_trials total_trials estimated_load_seconds estimated_total_seconds mode_count=0
	local expected_canaries=(12000 30000 60000 90000)
	if [[ $sli_only != true && $sli_only != false ]]; then
		echo "R3_SLI_ONLY must be true or false" >&2
		return 1
	fi
	if [[ $quick_throughput != true && $quick_throughput != false ]]; then
		echo "R3_QUICK_THROUGHPUT must be true or false" >&2
		return 1
	fi
	if [[ $fast_sustain_only != true && $fast_sustain_only != false ]]; then
		echo "R3_FAST_SUSTAIN_ONLY must be true or false" >&2
		return 1
	fi
	[[ $sli_only == true ]] && ((mode_count += 1))
	[[ $quick_throughput == true ]] && ((mode_count += 1))
	[[ $fast_sustain_only == true ]] && ((mode_count += 1))
	if ((mode_count > 1)); then
		echo "R3_SLI_ONLY, R3_QUICK_THROUGHPUT, and R3_FAST_SUSTAIN_ONLY are mutually exclusive" >&2
		return 1
	fi
	for value in "$calibration_seconds" "$ceiling_seconds" "$operating_seconds" \
		"$meta_cooldown_seconds" "$canary_seconds" "$health_freshness_seconds" "$quick_seconds" \
		"$fast_sustain_eps" "$fast_sustain_min_eps" "$fast_sustain_seconds" \
		"$fast_sustain_batch" "$fast_sustain_outstanding" "$normal_load_eps" \
		"$normal_load_max_p99_ms" "$loaded_max_p99_ms" "$max_broker_cpu_pct" \
		"$broker_cpu_limit_cores"; do
		if ! positive_integer "$value"; then
			echo "benchmark durations and cooldowns must be positive integer seconds" >&2
			return 1
		fi
	done
	if ! sli_seconds=$(duration_seconds "$sli_duration"); then
		echo "R3_SLI_DURATION must use positive whole h/m/s units (for example 3h or 1h30m)" >&2
		return 1
	fi
	read -r -a canary_rate_values <<<"$canary_rates"
	if ((${#canary_rate_values[@]} != ${#expected_canaries[@]})); then
		echo "R3_CANARY_RATES must be exactly: ${expected_canaries[*]}" >&2
		return 1
	fi
	for i in "${!expected_canaries[@]}"; do
		if [[ ${canary_rate_values[$i]} != "${expected_canaries[$i]}" ]]; then
			echo "R3_CANARY_RATES must be exactly: ${expected_canaries[*]}" >&2
			return 1
		fi
	done
	if [[ ${canary_rate_values[${#canary_rate_values[@]}-1]} != "$operating_eps" ]]; then
		echo "the exact canary ramp must end at the configured operating point ($operating_eps/s)" >&2
		return 1
	fi

	atomic_modes=$(jq -er '.atomic_batch_sizes | length' "$profile")
	fast_flows=$(jq -er '.fast_flows | length' "$profile")
	fast_outstanding=$(jq -er '.fast_outstanding_acks | length' "$profile")
	calibration_trials=$((1 + atomic_modes + fast_flows * fast_outstanding))
	total_trials=$((${#canary_rate_values[@]} + calibration_trials + 2))
	estimated_load_seconds=$((${#canary_rate_values[@]} * canary_seconds +
		calibration_trials * calibration_seconds + ceiling_seconds + operating_seconds))
	# Allow several bounded health snapshots, topology settling, and verified
	# cleanup per trial. PID 1 outlives both this estimate and the SLI duration.
	estimated_total_seconds=$((estimated_load_seconds + total_trials * (meta_cooldown_seconds + 600) + 600))
	if ((estimated_total_seconds < sli_seconds)); then
		estimated_total_seconds=$sli_seconds
	fi
	pod_lifetime_seconds=$((estimated_total_seconds + 1800))
}

write_sli_summary() {
	local i node_summary
	local node_summaries=()
	for i in "${!nodes[@]}"; do
		node_summary="$results_dir/sli-summary-${nodes[$i]}.json"
		jq -e -s --arg node "${nodes[$i]}" '
			def percentile($values; $rank):
				($values | sort) as $ordered |
				$ordered[((($ordered | length) * $rank) | ceil) - 1];
			def distribution($values): {
				min: ($values | min),
				p50: percentile($values; 0.50),
				p95: percentile($values; 0.95),
				p99: percentile($values; 0.99),
				max: ($values | max)
			};
			[.[] | .rpc[] | .rtt_ms] as $rpc |
			[.[] | .ingress.rtt_ms] as $ingress |
			[.[] | .valkey | .ping_rtt_ms, .set_rtt_ms, .get_rtt_ms] as $valkey |
			select(length > 0 and ($rpc | length) > 0) | {
				node: $node,
				cycles: length,
				rpc_samples: ($rpc | length),
				rpc_health_round_trip_ms: distribution($rpc),
				ingress_snapshot_ms: distribution($ingress),
				valkey_operation_ms: distribution($valkey),
				passed: ([.[] | .passed] | all)
			}
		' "${sli_monitor_files[$i]}" >"$node_summary"
		node_summaries+=("$node_summary")
	done
	jq -e -s '.' "${node_summaries[@]}" >"$results_dir/sli-summary.json"
	jq . "$results_dir/sli-summary.json"
}

setup_exec() {
  kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" exec "$control_pod" -- sh -c \
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
	  if ! wait_for_meta_idle post-delete "$health_monitor_file"; then
		return 1
	  fi
	  sleep "$meta_cooldown_seconds"
	  wait_for_meta_idle post-delete-cooldown "$health_monitor_file"
	  return
    fi
    sleep "$attempt"
  done
  echo "failed to verify deletion of shadow stream $current_stream after five attempts" >&2
  return 1
}

pods_are_absent() {
	local existing
	if ! existing=$(kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" \
		get pod "$@" --ignore-not-found -o name); then
		return 1
	fi
	[[ -z $existing ]]
}

delete_pods_verified() {
	local label=$1 timeout=$2 attempt
	shift 2
	local targets=("$@")
	local cleanup_log="$results_dir/pod-cleanup.log"
	for attempt in 1 2 3 4 5; do
		if pods_are_absent "${targets[@]}"; then
			return 0
		fi
		if ! kubectl -n "$namespace" delete pod "${targets[@]}" \
			--ignore-not-found --wait=true --timeout="$timeout" >>"$cleanup_log" 2>&1; then
			echo "attempt=$attempt failed to delete $label pods" >>"$cleanup_log"
		fi
		if pods_are_absent "${targets[@]}"; then
			return 0
		fi
		sleep "$attempt"
	done
	echo "failed to verify absence of $label pods after five attempts: ${targets[*]}" \
		| tee -a "$cleanup_log" >&2
	return 1
}

stop_publisher_pods() {
	if [[ $publisher_pods_created != true ]]; then
		return 0
	fi
	if ! delete_pods_verified publisher 30s "${pods[@]}"; then
		return 1
	fi
	publisher_pods_created=false
}

stop_sli_monitors() {
	local status=0 pid
	if [[ $sli_monitors_started == true ]]; then
		for pid in "${sli_monitor_pids[@]}"; do
			kill "$pid" >/dev/null 2>&1 || true
		done
		for pid in "${sli_monitor_pids[@]}"; do
			wait "$pid" >/dev/null 2>&1 || true
		done
		sli_monitor_pids=()
		sli_monitors_started=false
	fi
	if [[ $sli_pods_created == true ]]; then
		if delete_pods_verified sli 30s "${sli_pods[@]}"; then
			sli_pods_created=false
		else
			status=1
		fi
	fi
	return "$status"
}

stop_health_monitor() {
	if [[ $health_monitor_started != true ]]; then
		return 0
	fi
	: >"$health_monitor_stop"
	local status=0
	if ! wait "$health_monitor_pid"; then
		status=1
	fi
	health_monitor_started=false
	return "$status"
}

cleanup() {
  if [[ $cleanup_started == true ]]; then
	return "$cleanup_status"
  fi
  cleanup_started=true
	local status=0 publishers_stopped=true verify_run_pods=false
	if [[ $control_pod_created == true || $sli_pods_created == true || $publisher_pods_created == true ]]; then
		verify_run_pods=true
	fi
  if ((${#active_pids[@]} > 0)); then
    kill "${active_pids[@]}" >/dev/null 2>&1 || true
    for pid in "${active_pids[@]}"; do
      wait "$pid" >/dev/null 2>&1 || true
    done
    active_pids=()
  fi
	if [[ $publisher_pods_created == true ]]; then
		if ! stop_publisher_pods; then
			echo "retrying publisher pod deletion before stream cleanup" >&2
			if ! stop_publisher_pods; then
				publishers_stopped=false
				status=1
			fi
		fi
	fi
	if [[ $stream_created == true ]]; then
		if [[ $publishers_stopped != true ]]; then
			echo "publisher pod absence is unverified; preserving the isolated stream instead of deleting under active load" >&2
		elif ! delete_current_stream; then
			status=1
		fi
	fi
	if ! stop_sli_monitors; then status=1; fi
	if ! stop_health_monitor; then status=1; fi
	# Retry and verify every run-owned pod, including resources whose first
	# deletion response may have been lost. Clear ownership only after absence.
	if [[ $verify_run_pods == true ]]; then
		if delete_pods_verified all-run-owned 60s "${all_pods[@]}"; then
			control_pod_created=false
			sli_pods_created=false
			publisher_pods_created=false
		else
			status=1
		fi
	fi
  if ! rm -f "$binary"; then status=1; fi
  echo "R3 artifacts retained in $results_dir"
	cleanup_status=$status
	return "$cleanup_status"
}

handle_exit() {
	local status=$? cleanup_result=0
	trap - EXIT
	cleanup || cleanup_result=$?
	if ((status == 0 && cleanup_result != 0)); then
		status=$cleanup_result
	fi
	exit "$status"
}

exit_from_signal() {
  local status=$1
  exit "$status"
}

trap handle_exit EXIT
trap 'exit_from_signal 130' INT
trap 'exit_from_signal 143' TERM

create_pod() {
  local pod=$1 node=$2 role=$3 overrides
	overrides=$(jq -nc \
	    --arg pod "$pod" --arg node "$node" --arg image "$image" \
	    --arg setup_secret "$setup_secret" --arg publisher_secret "$publisher_secret" \
		--arg admin_rpc_secret "$admin_rpc_secret" \
		--arg priority_class "$priority_class" \
		--arg role "$role" --arg pod_lifetime "$pod_lifetime_seconds" \
		'{spec:{nodeSelector:{"kubernetes.io/hostname":$node},restartPolicy:"Never",priorityClassName:$priority_class,automountServiceAccountToken:false,
		tolerations:[{key:"itsbagelbot.dev/pool",operator:"Equal",value:"worker-pool",effect:"NoSchedule"}],
		containers:[{
	  name:$pod,image:$image,command:["sleep",$pod_lifetime],
	  env:([{name:"GOMAXPROCS",value:"1"}] + (if $role == "control" then [
		{name:"NATS_SETUP_USER",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_USER"}}},
		{name:"NATS_SETUP_PASSWORD",valueFrom:{secretKeyRef:{name:$setup_secret,key:"NATS_PASSWORD"}}}
	  ] elif $role == "sli" then [
		{name:"NATS_USER",valueFrom:{secretKeyRef:{name:$admin_rpc_secret,key:"NATS_RPC_USER"}}},
		{name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:$admin_rpc_secret,key:"NATS_RPC_PASSWORD"}}},
		{name:"NATS_RPC_URL",value:"tls://nats-leaf-local.production.svc.cluster.local:4222"},
		{name:"VALKEY_ADDR",value:"valkey.valkey.svc.cluster.local:26380"},
		{name:"VALKEY_PASSWORD",valueFrom:{secretKeyRef:{name:$publisher_secret,key:"VALKEY_PASSWORD"}}},
		{name:"VALKEY_TLS_CA_PEM",valueFrom:{configMapKeyRef:{name:"fleet-ca",key:"ca.pem"}}},
		{name:"VALKEY_TLS_SERVER_NAME",value:"valkey.valkey.svc.cluster.local"},
		{name:"VALKEY_LOCAL_ADDR",value:"valkey-local.valkey.svc.cluster.local:6380"},
		{name:"NODE_IP",valueFrom:{fieldRef:{fieldPath:"status.hostIP"}}}
	  ] else [
		{name:"NATS_USER",valueFrom:{secretKeyRef:{name:$publisher_secret,key:"NATS_USER"}}},
		{name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:$publisher_secret,key:"NATS_PASSWORD"}}}
	  ] end)),
      volumeMounts:[{name:"fleet-ca",mountPath:"/etc/nats-ca",readOnly:true}],
	  resources:(if $role == "publisher" then
		{requests:{cpu:"100m",memory:"256Mi"},limits:{cpu:"1",memory:"1Gi"}}
	  else
		{requests:{cpu:"25m",memory:"64Mi"},limits:{cpu:"250m",memory:"256Mi"}}
	  end),
      securityContext:{runAsUser:1000,runAsGroup:1000,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
    }],volumes:[{name:"fleet-ca",configMap:{name:"fleet-ca"}}],
    securityContext:{runAsNonRoot:true,runAsUser:1000,runAsGroup:1000,seccompProfile:{type:"RuntimeDefault"}}}}')
  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never \
	--overrides="$overrides" -- sleep "$pod_lifetime_seconds" >/dev/null
}

require_nats_topology() {
	nats_topology=$(kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" get pods -l app=nats -o json | jq -c '
    [.items[] | {
      server:.metadata.name,
      node:.spec.nodeName,
      ready:([.status.containerStatuses[]? | select(.name == "nats") | .ready] | any)
    }]
  ')
	if [[ $(jq '[.[] | select(.ready)] | length' <<<"$nats_topology") != 3 ]]; then
	  echo "R3 shadow requires exactly the three ready NATS servers in the capacity profile: $nats_topology" >&2
    exit 1
  fi
  for node in "${nodes[@]}"; do
	  if [[ $(jq --arg node "$node" '[.[] | select(.node == $node and .ready)] | length' <<<"$nats_topology") != 1 ]]; then
	    echo "expected exactly one ready NATS server on $node: $nats_topology" >&2
      exit 1
    fi
  done
  preferred_server=$(jq -er '.[] | select(.node == "node3") | .server' <<<"$nats_topology")
  forbidden_server=$(jq -er '.[] | select(.node == "worker1") | .server' <<<"$nats_topology")
  echo "NATS map: $nats_topology"
  echo "preferred leader on node3: $preferred_server; forbidden leader on worker1: $forbidden_server"
}

publisher_url_for_node() {
	local node=$1 server
	if [[ $publish_target == preferred ]]; then
		printf '%s\n' "$leader_url"
		return
	fi
	server=$(jq -er --arg node "$node" '.[] | select(.node == $node and .ready) | .server' \
		<<<"$nats_topology")
	printf 'tls://%s.nats-headless.%s.svc.cluster.local:4222\n' "$server" "$namespace"
}

extract_authoritative_stream_topologies() {
	local pod=$1
	jq -ce --arg pod "$pod" '[
		.account_details[]?.stream_detail[]?
		| select(
			.name == "BAGEL_DATA" or
			.name == "TWITCH_INGRESS" or
			.name == "TWITCH_OUTGRESS" or
			.name == "TWITCH_OUTGRESS_SYSTEM"
		)
		| select(.cluster.leader == $pod)
		| {
			stream:.name,
			leader:.cluster.leader,
			leader_since:(.cluster.leader_since // ""),
			raft_group:(.cluster.raft_group // ""),
			replicas:([
				.cluster.replicas[]?
				| {
					name:.name,
					peer:(.peer // ""),
					current:(.current // false),
					offline:(.offline // false),
					lag:(.lag // 0)
				}
			] | sort_by(.name, .peer))
		}
	]'
}

cluster_health_snapshot() {
	local deployments production_pods valkey_pods nats_pods pod varz routez jsz broker consumers topologies
	local brokers_json='[]' consumers_json='[]' topologies_json='[]'
	if ! deployments=$(kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" get deployments -o json); then
		echo "failed to query production Deployments within $deployment_query_timeout" >&2
		return 1
	fi
	if ! production_pods=$(kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" get pods -o json); then
		echo "failed to query production pods within $deployment_query_timeout" >&2
		return 1
	fi
	if ! valkey_pods=$(kubectl --request-timeout="$deployment_query_timeout" -n "$valkey_namespace" get pods -l app.kubernetes.io/name=valkey -o json); then
		echo "failed to query Valkey pods within $deployment_query_timeout" >&2
		return 1
	fi
	if ! nats_pods=$(kubectl --request-timeout="$broker_query_timeout" -n "$namespace" get pods -l app=nats -o json); then
		echo "failed to query NATS pods within $broker_query_timeout" >&2
		return 1
	fi
	while IFS= read -r pod; do
		if ! varz=$(kubectl --request-timeout="$broker_query_timeout" get --raw \
			"/api/v1/namespaces/${namespace}/pods/${pod}:8222/proxy/varz"); then
			echo "failed to query $pod varz within $broker_query_timeout" >&2
			return 1
		fi
		if ! jsz=$(kubectl --request-timeout="$broker_query_timeout" get --raw \
			"/api/v1/namespaces/${namespace}/pods/${pod}:8222/proxy/jsz?streams=1&consumers=1"); then
			echo "failed to query $pod jsz within $broker_query_timeout" >&2
			return 1
		fi
		if ! routez=$(kubectl --request-timeout="$broker_query_timeout" get --raw \
			"/api/v1/namespaces/${namespace}/pods/${pod}:8222/proxy/routez?subs=0"); then
			echo "failed to query $pod routez within $broker_query_timeout" >&2
			return 1
		fi
		if ! broker=$(jq -nce --arg pod "$pod" --argjson cpu_limit_cores "$broker_cpu_limit_cores" \
			--argjson varz "$varz" --argjson routez "$routez" '{
			name:$pod,
			cpu_pct:($varz.cpu // 0),
			cpu_cores:(($varz.cpu // 0) / 100),
			cpu_limit_utilization_pct:(($varz.cpu // 0) / $cpu_limit_cores),
			memory_bytes:($varz.mem // 0),
			connections:($varz.connections // 0),
			in_msgs:($varz.in_msgs // 0),
			out_msgs:($varz.out_msgs // 0),
			in_bytes:($varz.in_bytes // 0),
			out_bytes:($varz.out_bytes // 0),
			slow_consumers:($varz.slow_consumers // 0),
			meta_pending:($varz.jetstream.meta.pending // 0),
			meta_pending_requests:($varz.jetstream.meta.pending_requests // 0),
			meta_pending_infos:($varz.jetstream.meta.pending_infos // 0),
			routes:{
				count:(($routez.routes // []) | length),
				pending_bytes:([($routez.routes // [])[].pending_size] | add // 0),
				max_pending_bytes:([($routez.routes // [])[].pending_size] | max // 0),
				in_msgs:([($routez.routes // [])[].in_msgs] | add // 0),
				out_msgs:([($routez.routes // [])[].out_msgs] | add // 0),
				in_bytes:([($routez.routes // [])[].in_bytes] | add // 0),
				out_bytes:([($routez.routes // [])[].out_bytes] | add // 0)
			}
		}'); then
			return 1
		fi
		if ! consumers=$(jq -ce --arg pod "$pod" '[
			.account_details[]?.stream_detail[]?
			| select(
				.name == "BAGEL_DATA" or
				.name == "TWITCH_INGRESS" or
				.name == "TWITCH_OUTGRESS" or
				.name == "TWITCH_OUTGRESS_SYSTEM"
			) as $stream
			| $stream.consumer_detail[]?
			| {
				server:$pod,
				stream:$stream.name,
				name:.name,
				created:(.created // ""),
				pending:(.num_pending // 0),
				ack_pending:(.num_ack_pending // 0),
				redelivered:(.num_redelivered // 0)
			}
		]' <<<"$jsz"); then
			return 1
		fi
		if ! topologies=$(extract_authoritative_stream_topologies "$pod" <<<"$jsz"); then
			return 1
		fi
		brokers_json=$(jq -c --argjson broker "$broker" '. + [$broker]' <<<"$brokers_json")
		consumers_json=$(jq -c --argjson consumers "$consumers" '. + $consumers' <<<"$consumers_json")
		topologies_json=$(jq -c --argjson topologies "$topologies" '. + $topologies' <<<"$topologies_json")
	done < <(jq -r '.items[].metadata.name' <<<"$nats_pods")
	jq -n \
		--slurpfile deployments <(printf '%s\n' "$deployments") \
		--slurpfile production_pods <(printf '%s\n' "$production_pods") \
		--slurpfile valkey_pods <(printf '%s\n' "$valkey_pods") \
		--slurpfile nats_pods <(printf '%s\n' "$nats_pods") \
		--slurpfile brokers <(printf '%s\n' "$brokers_json") \
		--slurpfile consumers <(printf '%s\n' "$consumers_json") \
		--slurpfile topologies <(printf '%s\n' "$topologies_json") '
		{
			deployments: [
				$deployments[0].items[] | {
					name: .metadata.name,
					uid: .metadata.uid,
					desired: (.spec.replicas // 0),
					available: (.status.availableReplicas // 0),
					ready: (.status.readyReplicas // 0),
					updated: (.status.updatedReplicas // 0)
				}
			],
			production_pods: [
				$production_pods[0].items[]
				| select((.metadata.ownerReferences // []) | any(
					.kind == "ReplicaSet" or .kind == "StatefulSet" or .kind == "DaemonSet"
				))
				| (.status.containerStatuses // []) as $statuses
				| {
					name: .metadata.name,
					uid: .metadata.uid,
					node: .spec.nodeName,
					phase: .status.phase,
					ready: (($statuses | length) > 0 and ($statuses | all(.ready == true))),
					restarts: ([$statuses[].restartCount] | add // 0)
				}
			],
			valkey: [
				$valkey_pods[0].items[]
				| (.status.containerStatuses // []) as $statuses
				| {
					name: .metadata.name,
					uid: .metadata.uid,
					node: .spec.nodeName,
					phase: .status.phase,
					ready: (($statuses | length) > 0 and ($statuses | all(.ready == true))),
					restarts: ([$statuses[].restartCount] | add // 0)
				}
			],
			nats: [
				$nats_pods[0].items[] | {
					name: .metadata.name,
					uid: .metadata.uid,
					node: .spec.nodeName,
					phase: .status.phase,
					ready: ([.status.containerStatuses[]? | select(.name == "nats") | .ready] | any),
					restarts: ([.status.containerStatuses[]? | select(.name == "nats") | .restartCount] | add // 0)
				}
			],
			brokers: $brokers[0],
			stream_topologies: ($topologies[0] | sort_by(.stream)),
			consumer_details: $consumers[0],
			consumers: {
				count: ($consumers[0] | length),
				pending: ([$consumers[0][].pending] | add // 0),
				ack_pending: ([$consumers[0][].ack_pending] | add // 0),
				redelivered: ([$consumers[0][].redelivered] | add // 0)
			}
		}'
}

capture_health_baseline() {
	if ! cluster_health_snapshot >"$health_baseline_file"; then
		echo "failed to capture the production health baseline" >&2
		return 1
	fi
	if ! jq -e '
		(.deployments | length) > 0 and
		(all(.deployments[]; .desired == .available and .desired == .ready and .desired == .updated)) and
		(.production_pods | length) > 0 and
		(all(.production_pods[]; .phase == "Running" and .ready == true)) and
		(.valkey | length) == 4 and
		(all(.valkey[]; .phase == "Running" and .ready == true)) and
		(.nats | length) == 3 and
		(all(.nats[]; .phase == "Running" and .ready == true)) and
		(.brokers | length) == 3 and
			(all(.brokers[];
				.meta_pending == 0 and
				.meta_pending_requests == 0 and
				.meta_pending_infos == 0
			)) and
			([.stream_topologies[].stream] == [
				"BAGEL_DATA",
				"TWITCH_INGRESS",
				"TWITCH_OUTGRESS",
				"TWITCH_OUTGRESS_SYSTEM"
			]) and
			(all(.stream_topologies[];
				.leader != "" and
				(all(.replicas[]; .name != "" and .current == true and .offline == false and .lag == 0))
			)) and
			(.consumer_details | length) > 0 and
		(all(.consumer_details[]; .created != "" and .redelivered == 0)) and
		([
			"commands_data_commands_used",
			"commands_data_users_deleted",
			"loyalty_data_loyalty_counters",
			"loyalty_data_loyalty_earned",
			"loyalty_data_users_deleted",
			"modules_data_reproject_request",
			"modules_data_users_deleted",
			"outgress-system_twitch_outgress_system",
			"outgress-premium_twitch_outgress_premium",
			"outgress-standard_twitch_outgress_standard",
			"outgress_twitch_ingress_event_stream",
			"projector_data_commands_changed",
			"projector_data_modules_changed",
			"projector_data_users_changed",
			"projector_data_users_deleted",
			"projector_twitch_ingress_event_stream",
			"users_data_reproject_request",
			"worker_twitch_ingress_event_premium",
			"worker_twitch_ingress_event_standard"
		] - [.consumer_details[].name] | length) == 0 and
		(.consumers.count == (.consumer_details | length)) and
		(.consumers.redelivered == 0)
	' "$health_baseline_file" >/dev/null; then
		echo "production is not fully available; refusing to start shared-bus load" >&2
		jq . "$health_baseline_file" >&2 || true
		return 1
	fi
}

validate_health_snapshot() {
	jq -e \
		--argjson pending_growth "$consumer_pending_growth_max" \
		--argjson ack_pending_growth "$consumer_ack_pending_growth_max" \
		--slurpfile baseline "$health_baseline_file" '
		. as $now |
		$baseline[0] as $before |
		($now.deployments | length) == ($before.deployments | length) and
		(all($now.deployments[];
			.desired == .available and .desired == .ready and .desired == .updated
		)) and
		(all($before.deployments[]; . as $expected |
			$now.deployments | any(
				.name == $expected.name and
				.uid == $expected.uid and
				.desired == $expected.desired and
				.available == $expected.desired and
				.ready == $expected.desired and
				.updated == $expected.desired
			)
		)) and
		($now.production_pods | length) == ($before.production_pods | length) and
		(all($before.production_pods[]; . as $expected |
			$now.production_pods | any(
				.name == $expected.name and
				.uid == $expected.uid and
				.phase == "Running" and
				.ready == true and
				.restarts == $expected.restarts
			)
		)) and
		($now.valkey | length) == 4 and
		(all($before.valkey[]; . as $expected |
			$now.valkey | any(
				.name == $expected.name and
				.uid == $expected.uid and
				.phase == "Running" and
				.ready == true and
				.restarts == $expected.restarts
			)
		)) and
		($now.nats | length) == 3 and
		(all($before.nats[]; . as $expected |
			$now.nats | any(
				.name == $expected.name and
				.uid == $expected.uid and
				.phase == "Running" and
				.ready == true and
				.restarts == $expected.restarts
			)
		)) and
		(all($before.brokers[]; . as $expected |
			$now.brokers | any(
				.name == $expected.name and
				.slow_consumers == $expected.slow_consumers
			)
		)) and
		($now.brokers | length) == ($before.brokers | length) and
		($now.stream_topologies | length) == ($before.stream_topologies | length) and
		(all($now.stream_topologies[];
			.leader != "" and
			(all(.replicas[];
				.name != "" and .current == true and .offline == false and .lag == 0
			))
		)) and
		(all($before.stream_topologies[]; . as $expected |
			$now.stream_topologies | any(
				.stream == $expected.stream and
				.leader == $expected.leader and
				.leader_since == $expected.leader_since and
				.raft_group == $expected.raft_group and
				.replicas == $expected.replicas
			)
		)) and
		(all($before.consumer_details[]; . as $expected |
			$now.consumer_details | any(
				.server == $expected.server and
				.stream == $expected.stream and
				.name == $expected.name and
				.created == $expected.created and
				.pending <= $expected.pending + $pending_growth and
				.ack_pending <= $expected.ack_pending + $ack_pending_growth and
				.redelivered == 0
			)
		)) and
		($now.consumers.count == $before.consumers.count) and
		($now.consumers.pending <= $before.consumers.pending + $pending_growth) and
		($now.consumers.ack_pending <= $before.consumers.ack_pending + $ack_pending_growth) and
		($now.consumers.redelivered == 0)
	' >/dev/null
}

validate_meta_idle_snapshot() {
	jq -e '
		(.brokers | length) == 3 and
		(all(.brokers[];
			.meta_pending == 0 and
			.meta_pending_requests == 0 and
			.meta_pending_infos == 0
		))
	' >/dev/null
}

check_recent_nats_logs() {
	local log_file=$1 recent
	if ! recent=$(kubectl --request-timeout="$broker_query_timeout" -n "$namespace" logs -l app=nats -c nats \
		--prefix --since=5m --max-log-requests=3); then
		echo "failed to query recent NATS logs" >>"$log_file"
		return 1
	fi
	if grep -Eina \
		'IPQ len limit|slow consumer|write deadline( exceeded)?|no quorum|quorum (lost|unavailable)' \
		<<<"$recent" >>"$log_file"; then
		echo "NATS emitted a live queue, slow-consumer, write-deadline, or quorum error" \
			>>"$log_file"
		return 1
	fi
}

health_checkpoint() {
	local log_file=$1 phase=$2 snapshot observed_at
	observed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	if ! snapshot=$(cluster_health_snapshot); then
		echo "$observed_at phase=$phase failed to query production readiness" >>"$log_file"
		return 1
	fi
	jq -c --arg observed_at "$observed_at" --arg phase "$phase" \
		'. + {observed_at:$observed_at,phase:$phase}' <<<"$snapshot" >>"$log_file"
	if ! validate_health_snapshot <<<"$snapshot"; then
		echo "$observed_at phase=$phase production availability, pod identity, broker health, or consumer lag changed" \
			>>"$log_file"
		return 1
	fi
	check_recent_nats_logs "$log_file"
}

monitor_cluster_health() {
	local stop_file=$1 ready_file=$2 log_file=$3 cycle=0 i
	while [[ ! -e $stop_file ]]; do
		cycle=$((cycle + 1))
		if ! health_checkpoint "$log_file" "continuous-$cycle"; then
			return 1
		fi
		date +%s >"$ready_file"
		for ((i = 0; i < health_poll_seconds; i++)); do
			if [[ -e $stop_file ]]; then
				return 0
			fi
			sleep 1
		done
	done
}

wait_for_monitor_ready() {
	local pid=$1 ready_file=$2 label=$3 deadline=$((SECONDS + 90))
	while [[ ! -e $ready_file ]]; do
		if ! kill -0 "$pid" 2>/dev/null; then
			echo "$label monitor exited before its first successful sample" >&2
			return 1
		fi
		if ((SECONDS >= deadline)); then
			echo "$label monitor did not complete its first sample within 90s" >&2
			return 1
		fi
		sleep 0.2
	done
}

start_sli_monitors() {
	local i
	sli_monitor_pids=()
	for i in "${!nodes[@]}"; do
		rm -f "${sli_monitor_ready_files[$i]}"
		kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" \
			exec "${sli_pods[$i]}" -- \
			env NATS_CA=/etc/nats-ca/ca.pem \
			/tmp/nats-live-acceptance \
			-sli-only=true -duration="$sli_duration" -interval="$sli_interval" \
			-timeout=2s -max-rtt="$sli_max_rtt" \
			-ingress-max-rtt="$sli_ingress_max_rtt" \
			-rpc-p99-max="$sli_rpc_p99_max" \
			-rpc-p99-min-samples="$sli_rpc_p99_min_samples" \
			-producer-id="r3-shadow-sli-${nodes[$i]}" \
			-key="acceptance:sli:${run_id}:${nodes[$i]}" \
			>"${sli_monitor_files[$i]}" 2>"${sli_monitor_errs[$i]}" &
		sli_monitor_pids+=("$!")
	done
	sli_monitors_started=true
}

wait_for_sli_lane_ready() {
	local i=$1 deadline=$((SECONDS + 300))
	while :; do
		if [[ -s ${sli_monitor_files[$i]} ]] && jq -e -s \
			--argjson min "$sli_rpc_p99_min_samples" '
			length > 0 and .[-1].rpc_p99_samples >= $min and
			([.[] | select(.passed == false)] | length) == 0
		' "${sli_monitor_files[$i]}" >/dev/null 2>&1; then
			: >"${sli_monitor_ready_files[$i]}"
			return 0
		fi
		if ! kill -0 "${sli_monitor_pids[$i]}" 2>/dev/null; then
			echo "${nodes[$i]} local RPC/Valkey SLI exited before its first passing sample" >&2
			tail -20 "${sli_monitor_files[$i]}" "${sli_monitor_errs[$i]}" >&2 || true
			return 1
		fi
		if ((SECONDS >= deadline)); then
			echo "${nodes[$i]} local RPC/Valkey SLI did not arm the RPC p99 gate within 300s" >&2
			return 1
		fi
		sleep 0.2
	done
}

wait_for_sli_ready() {
	local i
	for i in "${!nodes[@]}"; do
		wait_for_sli_lane_ready "$i" || return 1
	done
}

assert_monitor_alive() {
	local pid=$1 label=$2
	if ! kill -0 "$pid" 2>/dev/null; then
		echo "$label monitor is no longer running" >&2
		return 1
	fi
}

assert_health_monitor_fresh() {
	local observed now
	if [[ ! -s $health_monitor_ready ]] || ! read -r observed <"$health_monitor_ready" ||
		! [[ $observed =~ ^[0-9]+$ ]]; then
		echo "production-health monitor has no valid successful-sample heartbeat" >&2
		return 1
	fi
	now=$(date +%s)
	if ((now - observed > health_freshness_seconds)); then
		echo "production-health monitor has not completed a sample for $((now - observed))s" >&2
		return 1
	fi
}

assert_safety_monitors() {
	if [[ $health_monitor_started == true ]]; then
		assert_monitor_alive "$health_monitor_pid" production-health || return 1
		assert_health_monitor_fresh || return 1
	fi
	if [[ $sli_monitors_started == true ]]; then
		local i
		for i in "${!sli_monitor_pids[@]}"; do
			assert_monitor_alive "${sli_monitor_pids[$i]}" \
				"${nodes[$i]}-local-rpc-valkey-sli" || return 1
		done
	fi
}

wait_for_meta_idle() {
	local phase=$1 log_file=$2 deadline=$((SECONDS + 45)) snapshot observed_at
	while :; do
		if ! assert_safety_monitors; then
			echo "phase=$phase safety monitor exited while waiting for JetStream meta idle" \
				>>"$log_file"
			return 1
		fi
		observed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
		if ! snapshot=$(cluster_health_snapshot); then
			echo "$observed_at phase=$phase failed to query production health" >>"$log_file"
			return 1
		fi
		if ! validate_health_snapshot <<<"$snapshot"; then
			echo "$observed_at phase=$phase detected a production health delta" >>"$log_file"
			return 1
		fi
		if validate_meta_idle_snapshot <<<"$snapshot"; then
			jq -c --arg observed_at "$observed_at" --arg phase "$phase" \
				'. + {observed_at:$observed_at,phase:$phase,meta_idle:true}' \
				<<<"$snapshot" >>"$log_file"
			return 0
		fi
		if ((SECONDS >= deadline)); then
			echo "$observed_at phase=$phase did not return to a healthy, meta-idle state within 45s" \
				>>"$log_file"
			return 1
		fi
		sleep 1
	done
}

prepare_trial_stream() {
  local label=$1 topology_file=$2 leader
	if ! assert_safety_monitors ||
		! wait_for_meta_idle pre-create "$health_monitor_file"; then
		return 2
	fi
	current_stream="$shadow_stream"
  current_subject="twitch.outgress.bench.r3.${run_id}.${label}"
  # Mark ownership before the request so a response lost after server-side
  # creation still takes the verified cleanup path.
  stream_created=true

  if ! setup_exec /tmp/nats-live-acceptance \
    -domain= -replicas=3 -required-peers=3 \
	    -stream "$current_stream" -subject "$current_subject" \
	    -setup-only=true -cleanup=false >/dev/null; then
	    return 2
  fi
  if ! setup_exec /tmp/nats-live-acceptance \
    -domain= -replicas=3 -required-peers=3 \
    -stream "$current_stream" -subject "$current_subject" \
    -create-stream=false -cleanup=false -topology-only=true \
	    -preferred-leader="$preferred_server" -forbidden-leader="$forbidden_server" \
	    -topology-duration=0 >"$topology_file"; then
	    return 2
	  fi
	  if ! leader=$(jq -er '.topology.leader' "$topology_file"); then
	    return 2
	  fi
	if ! assert_safety_monitors ||
		! wait_for_meta_idle post-create "$health_monitor_file"; then
		return 2
	fi
  leader_url="tls://${leader}.nats-headless.${namespace}.svc.cluster.local:4222"
}

messages_for_node() {
	local total=$1 index=$2 share
	share=$((total / 3))
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
	kubectl --request-timeout="$deployment_query_timeout" -n "$namespace" logs -l app=nats -c nats --prefix \
    --since-time="$test_started_at" >"$log_file"
  if grep -Eina \
    'IPQ len limit|slow consumer|write deadline( exceeded)?|no quorum|quorum (lost|unavailable)' \
    "$log_file"; then
    echo "NATS emitted a forbidden queue, slow-consumer, write-deadline, or quorum error" >&2
    return 1
  fi
}

summarize_trial() {
  local label=$1 target=$2 minimum=$3 expected_seconds=$4 compatible=$5 topology_file=$6 trial_max_p99_ms=$7
	shift 7
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
		--argjson max_p99_ms "$trial_max_p99_ms" \
		--argjson max_broker_cpu_pct "$max_broker_cpu_pct" \
		--arg publish_target "$publish_target" \
		--slurpfile health "$health_monitor_file" \
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
	  ([$health[] |
		select(((.observed_at | fromdateiso8601) * 1000) >= $started and
		       ((.observed_at | fromdateiso8601) * 1000) <= $finished) |
		.brokers[]]) as $broker_samples |
	  ($broker_samples | group_by(.name) | map({
		name:.[0].name,
		samples:length,
		max_cpu_cores:(map(.cpu_cores) | max),
		max_cpu_limit_utilization_pct:(map(.cpu_limit_utilization_pct) | max),
		max_memory_bytes:(map(.memory_bytes) | max),
		max_route_pending_bytes:(map(.routes.max_pending_bytes) | max),
		max_total_route_pending_bytes:(map(.routes.pending_bytes) | max)
	  })) as $broker_metrics |
      {
        trial:$label,
		stream_replicas:3,
		publish_target:$publish_target,
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
		puback_p99_gate_ms:$max_p99_ms,
        puback_max_ms:($r | map(.puback_max_ms) | max),
		max_publish_progress_gap_ms:($r | map(.max_publish_progress_gap_ms) | max),
        errors:$errors,
        timeouts:$timeouts,
        reconnects:$reconnects,
        disconnects:$disconnects,
        async_errors:$async_errors,
		broker_metrics:{
		  gate_cpu_pct:$max_broker_cpu_pct,
		  sample_count:($broker_samples | length),
		  peak_cpu_cores:([$broker_samples[].cpu_cores] | max // null),
		  peak_cpu_limit_utilization_pct:([$broker_samples[].cpu_limit_utilization_pct] | max // null),
		  peak_memory_bytes:([$broker_samples[].memory_bytes] | max // null),
		  peak_route_pending_bytes:([$broker_samples[].routes.max_pending_bytes] | max // null),
		  brokers:$broker_metrics
		},
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
		  ($broker_samples | length) >= 3 and
		  ([$broker_samples[].cpu_limit_utilization_pct] | max) <= $max_broker_cpu_pct and
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
  local max_seconds=$2
  shift 2
  local publisher_pids=("$@")
  local publisher_done=()
  local topology_done=false
	local remaining=$((1 + ${#publisher_pids[@]})) failed=0
  local deadline=$((SECONDS + max_seconds))
  local i pid progressed
  for _ in "${publisher_pids[@]}"; do publisher_done+=(false); done

  while ((remaining > 0)); do
	if ! assert_safety_monitors; then
		failed=2
	fi
	if ((SECONDS >= deadline)); then
		echo "trial watchdog expired after ${max_seconds}s" >&2
		failed=2
	fi
    progressed=false
    if [[ $topology_done == false ]] && ! kill -0 "$topology_pid" 2>/dev/null; then
		if ! wait "$topology_pid"; then failed=2; fi
      topology_done=true
      remaining=$((remaining - 1))
      progressed=true
    fi
    for i in "${!publisher_pids[@]}"; do
      pid=${publisher_pids[$i]}
      if [[ ${publisher_done[$i]} == false ]] && ! kill -0 "$pid" 2>/dev/null; then
			if ! wait "$pid" && ((failed == 0)); then failed=1; fi
        publisher_done[$i]=true
        remaining=$((remaining - 1))
        progressed=true
      fi
    done
		if ((failed == 2)); then
		kill "$topology_pid" "${publisher_pids[@]}" >/dev/null 2>&1 || true
      wait "$topology_pid" >/dev/null 2>&1 || true
      for pid in "${publisher_pids[@]}"; do wait "$pid" >/dev/null 2>&1 || true; done
		active_pids=()
      return 2
    fi
    if [[ $progressed == false ]]; then sleep 0.2; fi
  done
	active_pids=()
	return "$failed"
}

publisher_results_complete() {
	local label=$1 node file
	for node in "${nodes[@]}"; do
		file="$results_dir/${label}-${node}.json"
		if ! jq -e '
			(.results | length) == 1 and
			(.results[0].passed | type) == "boolean" and
			.results[0].started_unix_ms > 0 and
			.results[0].finished_unix_ms > .results[0].started_unix_ms
		' "$file" >/dev/null 2>&1; then
			return 1
		fi
	done
}

report_recent_health() {
	if [[ ! -s $health_monitor_file ]]; then
		return 0
	fi
	tail -3 "$health_monitor_file" | jq -c '{
		observed_at,
		phase,
		deployments_healthy:all(.deployments[];
			.desired == .available and .desired == .ready and .desired == .updated),
		production_pods:(.production_pods | length),
		nats_ready:([.nats[] | select(.ready == true)] | length),
		valkey_ready:([.valkey[] | select(.ready == true)] | length),
		stream_leaders:[.stream_topologies[] | {stream,leader}],
		consumer_pending:.consumers.pending,
		consumer_ack_pending:.consumers.ack_pending,
		consumer_redelivered:.consumers.redelivered
	}' >&2 || true
}

run_trial() {
  local label=$1 mode=$2 batch_size=$3 fast_outstanding=$4
  local target=$5 total_messages=$6 load_seconds=$7 minimum=$8 compatible=$9
  local topology_file="$results_dir/${label}-topology.json"
  local topology_err="$results_dir/${label}-topology.err"
	local start_ms node_target monitor_seconds latency_samples run_timeout_seconds ack_gap
	local trial_max_p99_ms publisher_url
	local prepare_status=0

	prepare_trial_stream "$label" "$topology_file" || prepare_status=$?
	if ((prepare_status != 0)); then
		if ! delete_current_stream; then return 2; fi
		return 2
	fi
  start_ms=$((($(date +%s) + 5) * 1000))
  # Cover the full 110% duration acceptance window, then leave 15 seconds for
  # follower catch-up. A run slow enough to outlive this monitor cannot pass.
  monitor_seconds=$((load_seconds * 11 / 10 + 15))
	run_timeout_seconds=$((load_seconds * 11 / 10 + 10))
  latency_samples=$((load_seconds * latency_hz))
  if ((latency_samples < 500)); then latency_samples=500; fi
  node_target=$(target_for_node "$target")
	ack_gap="$max_ack_gap"
	if [[ $mode == fast ]]; then ack_gap=0s; fi
	if ((target <= normal_load_eps)); then
		trial_max_p99_ms=$normal_load_max_p99_ms
	else
		trial_max_p99_ms=$loaded_max_p99_ms
	fi

	setup_exec /tmp/nats-live-acceptance \
    -hub-url="$leader_url" -domain= -replicas=3 -required-peers=3 \
    -stream "$current_stream" -subject "$current_subject" \
	    -create-stream=false -cleanup=false -topology-only=true \
	    -preferred-leader="$preferred_server" -forbidden-leader="$forbidden_server" \
	    -topology-unhealthy-grace="$topology_unhealthy_grace" \
	    -start-at-unix-ms="$start_ms" -topology-duration="${monitor_seconds}s" \
    >"$topology_file" 2>"$topology_err" &
	local topology_pid=$!
	active_pids=("$topology_pid")

  echo "trial=$label mode=$mode batch=$batch_size target=$target messages=$total_messages publish_target=$publish_target leader=$leader_url p99_gate=${trial_max_p99_ms}ms"
  local pids=()
  for i in "${!nodes[@]}"; do
    local node_messages
    node_messages=$(messages_for_node "$total_messages" "$i")
	 publisher_url=$(publisher_url_for_node "${nodes[$i]}")
    kubectl -n "$namespace" exec "${pods[$i]}" -- env NATS_CA=/etc/nats-ca/ca.pem \
      /tmp/nats-live-acceptance \
      -hub-url="$publisher_url" -domain= -replicas=3 -required-peers=3 \
      -stream "$current_stream" -subject "$current_subject" \
      -create-stream=false -cleanup=false \
      -producer-id="${nodes[$i]}" -messages="$node_messages" \
      -publishers="$publishers_per_node" -window="$window" \
      -payload-bytes="$payload_bytes" -payload-variants="$payload_variants" \
      -mode="$mode" -batch-size="$batch_size" -atomic-inflight="$atomic_inflight" \
      -fast-outstanding-acks="$fast_outstanding" -target-rate="$node_target" \
      -start-at-unix-ms="$start_ms" -latency-samples="$latency_samples" \
			-run-timeout="${run_timeout_seconds}s" \
			-max-ack-gap="$ack_gap" \
      -latency-interval=50ms -max-p95="${max_p95_ms}ms" -max-p99="${trial_max_p99_ms}ms" -min-rate=0 \
      >"$results_dir/${label}-${nodes[$i]}.json" \
      2>"$results_dir/${label}-${nodes[$i]}.err" &
    pids+=("$!")
		active_pids+=("$!")
  done

	local supervision_status=0
	supervise_trial_processes \
		"$topology_pid" "$((monitor_seconds + 45))" "${pids[@]}" || supervision_status=$?
	if ((supervision_status != 0)); then
		if ((supervision_status == 1)) && ! publisher_results_complete "$label"; then
			echo "publisher exec ended without a complete benchmark result; treating it as transport loss" >&2
			supervision_status=2
		fi
    for file in "$results_dir/${label}"-*.err; do
      sed "s#^#$(basename "$file"): #" "$file" >&2 || true
    done
		report_recent_health
			if ((supervision_status == 2)); then
			# A lost exec stream or safety monitor means a remote publisher may still
			# be running. Delete and wait for all publisher pods before stream cleanup;
			# the dedicated controller keeps the exact-scoped setup credential alive.
				if ! stop_publisher_pods; then
					echo "publisher pod absence could not be verified; preserving the stream and aborting" >&2
					return 2
				fi
		fi
		if ! delete_current_stream; then return 2; fi
		if ((supervision_status == 2)); then
			# A lost local exec stream does not prove the remote process exited. Abort
			# the matrix so the EXIT trap deletes every generator pod before any next
			# trial can overlap it.
			return 2
		fi
		return 1
  fi

  local summary_file="$results_dir/summary-${label}.json"
  local result_files=()
  for node in "${nodes[@]}"; do
    result_files+=("$results_dir/${label}-${node}.json")
  done
  if ! summarize_trial \
    "$label" "$target" "$minimum" "$load_seconds" "$compatible" "$topology_file" "$trial_max_p99_ms" \
    "${result_files[@]}" >"$summary_file"; then
    if delete_current_stream; then return 1; else return 2; fi
  fi
  if ! jq . "$summary_file"; then
    if delete_current_stream; then return 1; else return 2; fi
  fi
	if ! wait_for_meta_idle pre-delete "$health_monitor_file"; then
			if ! stop_publisher_pods; then
				echo "publisher pod absence could not be verified; preserving the stream and aborting" >&2
				return 2
			fi
			if ! delete_current_stream; then return 2; fi
			return 2
		fi
  if ! delete_current_stream; then return 2; fi
  jq -e '.passed' "$summary_file" >/dev/null
}

if [[ ${R3_PREFLIGHT_ONLY:-false} == true ]]; then
	require_nats_topology
	capture_health_baseline
	if ! preflight_snapshot=$(cluster_health_snapshot); then
		echo "R3 preflight could not capture its verification sample" >&2
		exit 1
	fi
	if ! validate_health_snapshot <<<"$preflight_snapshot"; then
		echo "R3 preflight detected a production health delta between samples" >&2
		jq . <<<"$preflight_snapshot" >&2 || true
		exit 1
	fi
	if ! validate_meta_idle_snapshot <<<"$preflight_snapshot"; then
		echo "R3 preflight found JetStream meta work in flight" >&2
		exit 1
	fi
	if ! check_recent_nats_logs "$results_dir/preflight-nats.log"; then
		echo "R3 preflight found a recent NATS safety event" >&2
		exit 1
	fi
	jq '{
		deployments:(.deployments | length),
		production_pods:(.production_pods | length),
		valkey_pods:(.valkey | length),
		nats_pods:(.nats | length),
		critical_consumer_rows:.consumers.count,
		consumer_pending:.consumers.pending,
		consumer_ack_pending:.consumers.ack_pending,
		consumer_redelivered:.consumers.redelivered
	}' "$health_baseline_file"
	echo "R3 production health preflight passed; no benchmark resources were created"
	exit 0
fi

configure_run_lifetime_and_canaries
echo "run-owned pod lifetime: ${pod_lifetime_seconds}s"

if ! kubectl get priorityclass "$priority_class" -o json 2>/dev/null | jq -e '
	.preemptionPolicy == "Never" and (.globalDefault // false) == false and .value <= 0
' >/dev/null; then
	echo "R3 benchmark priority class $priority_class must exist with value <= 0 and preemptionPolicy Never" >&2
	exit 1
fi

echo "building static NATS 2.14 acceptance binary (no Docker invocation)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$binary" ./deploy/k8s/nats-live-acceptance

if [[ $sli_only != true ]]; then
	if ! kubectl -n "$namespace" get secret "$setup_secret" >/dev/null 2>&1; then
		echo "missing temporary JetStream setup secret $setup_secret (see README.md)" >&2
		exit 1
	fi
fi
if ! kubectl -n "$namespace" get secret "$publisher_secret" >/dev/null 2>&1; then
	echo "missing publisher/Valkey credential Secret $publisher_secret" >&2
	exit 1
fi
if ! kubectl -n "$namespace" get secret "$admin_rpc_secret" >/dev/null 2>&1; then
	echo "missing admin RPC credential Secret $admin_rpc_secret" >&2
	exit 1
fi

require_nats_topology
capture_health_baseline
	rm -f "$health_monitor_stop" "$health_monitor_ready"
	monitor_cluster_health "$health_monitor_stop" "$health_monitor_ready" "$health_monitor_file" &
	health_monitor_pid=$!
	health_monitor_started=true
	if ! wait_for_monitor_ready "$health_monitor_pid" "$health_monitor_ready" production-health; then
		exit 1
	fi

sli_pods_created=true
for i in "${!nodes[@]}"; do
	create_pod "${sli_pods[$i]}" "${nodes[$i]}" sli
done
run_pods=("${sli_pods[@]}")
if [[ $sli_only != true ]]; then
	control_pod_created=true
	create_pod "$control_pod" node3 control
	publisher_pods_created=true
	for i in "${!nodes[@]}"; do
		create_pod "${pods[$i]}" "${nodes[$i]}" publisher
	done
	run_pods+=("$control_pod" "${pods[@]}")
fi
for pod in "${run_pods[@]}"; do
  kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
  kubectl -n "$namespace" cp "$binary" "$pod:/tmp/nats-live-acceptance"
	assert_monitor_alive "$health_monitor_pid" production-health
done

start_sli_monitors
if ! wait_for_sli_ready; then
	exit 1
fi
assert_safety_monitors
wait_for_meta_idle before-first-stream "$health_monitor_file"
if [[ $sli_only == true ]]; then
	write_sli_summary
	stop_sli_monitors
	stop_health_monitor
	echo "RPC/Valkey SLI-only qualification passed; no stream or publisher pod was created"
	exit 0
fi

if [[ $fast_sustain_only == true ]]; then
	fast_sustain_messages=$((fast_sustain_eps * fast_sustain_seconds))
	run_trial "fast-sustain-${fast_sustain_eps}" fast "$fast_sustain_batch" \
		"$fast_sustain_outstanding" "$fast_sustain_eps" "$fast_sustain_messages" \
		"$fast_sustain_seconds" "$fast_sustain_min_eps" false
	check_nats_logs
	assert_safety_monitors
	wait_for_meta_idle fast-sustain-final "$health_monitor_file"
	stop_sli_monitors
	stop_health_monitor
	jq . "$results_dir/summary-fast-sustain-${fast_sustain_eps}.json"
	echo "R3 Fast-Ingest sustain passed at ${fast_sustain_eps} events/s"
	exit 0
fi

async_label_batch=$(jq -er '.atomic_batch_sizes[1]' "$profile")
default_outstanding=$(jq -er '.fast_outstanding_acks[0]' "$profile")

for canary_rate in "${canary_rate_values[@]}"; do
	canary_messages=$((canary_rate * canary_seconds))
	canary_min=$((canary_rate * 99 / 100))
	if ! run_trial "canary-${canary_rate}" async "$async_label_batch" "$default_outstanding" \
		"$canary_rate" "$canary_messages" "$canary_seconds" "$canary_min" true; then
		echo "R3 canary ramp failed at ${canary_rate}/s; full qualification will not start" >&2
		exit 1
	fi
done
check_nats_logs
echo "R3 canary ramp passed: ${canary_rate_values[*]} events/s"
if [[ $quick_throughput == true ]]; then
	quick_messages=$((ceiling_offered_eps * quick_seconds))
	quick_status=0
	if run_trial "quick-async-${ceiling_offered_eps}" async "$async_label_batch" "$default_outstanding" \
		"$ceiling_offered_eps" "$quick_messages" "$quick_seconds" "$rated_eps" true; then
		:
	else
		quick_status=$?
	fi
	check_nats_logs
	assert_safety_monitors
	jq . "$results_dir/summary-quick-async-${ceiling_offered_eps}.json"
	if ((quick_status != 0)); then
		echo "bounded R3 throughput trial did not qualify" >&2
		exit "$quick_status"
	fi
	echo "bounded R3 throughput trial passed at ${ceiling_offered_eps} offered events/s"
	exit 0
fi
if [[ ${R3_CANARY_ONLY:-false} == true ]]; then
	echo "R3 canary-only run complete; full 120k qualification was not started"
	exit 0
fi

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
		"$calibration_target_eps" "$calibration_messages" "$calibration_seconds" 0 "$compatible"; then
    :
  else
	    trial_status=$?
	    if ((trial_status == 2)); then
	      echo "calibration $label hit a safety, topology, transport, or cleanup failure; aborting the matrix" >&2
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
	if length == 0 then null else
		min_by([.worst_node_puback_p99_ms, .worst_node_puback_p95_ms, (-.aggregate_messages_per_second)])
	end
' "${calibration_summaries[@]}")
if [[ $qualifier == null ]]; then
  echo "no Elixir-compatible async/atomic calibration passed" >&2
  exit 1
fi
qualifier_mode=$(jq -r '.mode | split("+")[0]' <<<"$qualifier")
qualifier_batch=$(jq -er '.batch_size' <<<"$qualifier")
qualifier_outstanding=$default_outstanding
echo "selected lowest-tail-latency Elixir-compatible qualifier: $qualifier_mode batch=$qualifier_batch"

ceiling_messages=$((ceiling_offered_eps * ceiling_seconds))
run_trial "ceiling-${qualifier_mode}-${qualifier_batch}" \
  "$qualifier_mode" "$qualifier_batch" "$qualifier_outstanding" \
  "$ceiling_offered_eps" "$ceiling_messages" "$ceiling_seconds" "$rated_eps" true

operating_messages=$((operating_eps * operating_seconds))
run_trial "operating-${qualifier_mode}-${qualifier_batch}" \
  "$qualifier_mode" "$qualifier_batch" "$qualifier_outstanding" \
  "$operating_eps" "$operating_messages" "$operating_seconds" "$operating_min_eps" true

check_nats_logs
assert_safety_monitors
wait_for_meta_idle final-zero-load "$health_monitor_file"
sleep "$meta_cooldown_seconds"
wait_for_meta_idle final-zero-load-cooldown "$health_monitor_file"
stop_sli_monitors
stop_health_monitor
echo "R3 qualification passed: rated=${rated_eps}/s operating=${operating_eps}/s (75%)"
