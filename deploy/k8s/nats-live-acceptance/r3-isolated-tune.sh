#!/usr/bin/env bash
# Qualify candidate NATS route settings on the production hardware without
# changing or publishing into the production NATS quorum.
set -euo pipefail

phase=bootstrap
report_error() {
	local status=$?
	echo "isolated R3 failure during phase=$phase at line $1 (exit $status)" >&2
	return "$status"
}
trap 'report_error "$LINENO"' ERR

if [[ ${CONFIRM_R3_ISOLATED_TUNE:-} != R3-ISOLATED-90K ]]; then
	cat <<'PLAN'
Isolated R3 tuning plan (no actions performed):
  - create a temporary native-TLS NATS 2.14 quorum on node2/node3/worker1
  - use a dedicated BUS account route with selectable NATS S2 compression
  - keep the production-shaped 400k per-subject rolling retention limit
  - publish 90,000 events/s through three node-local Fast-Ingest clients
  - require R3 followers to remain current with zero final lag
  - remove the stream, quorum, credentials, certificates, and test pods on exit

Set CONFIRM_R3_ISOLATED_TUNE=R3-ISOLATED-90K to run it.
PLAN
	exit 0
fi

for command in go jq kubectl openssl; do
	command -v "$command" >/dev/null 2>&1 || { echo "missing required command: $command" >&2; exit 1; }
done

namespace=${NAMESPACE:-production}
image=${R3_ISOLATED_NATS_IMAGE:-nats:2.14.3-alpine}
seconds=${R3_ISOLATED_SECONDS:-300}
fleet_rate=${R3_ISOLATED_RATE:-90000}
min_rate=${R3_ISOLATED_MIN_RATE:-89100}
batch=${R3_ISOLATED_BATCH:-1000}
outstanding=${R3_ISOLATED_OUTSTANDING:-8}
publisher_connections=${R3_ISOLATED_PUBLISHERS:-2}
publish_target=${R3_ISOLATED_PUBLISH_TARGET:-local}
publish_mode=${R3_ISOLATED_PUBLISH_MODE:-fast}
stream_replicas=${R3_ISOLATED_STREAM_REPLICAS:-3}
route_pool_size=${R3_ISOLATED_ROUTE_POOL_SIZE:-3}
preferred_node=${R3_ISOLATED_PREFERRED_NODE:-node3}
max_msgs_per_subject=${R3_ISOLATED_MAX_MSGS_PER_SUBJECT:-400000}
compression=${R3_ISOLATED_COMPRESSION:-s2_fast}
puback_p99=${R3_ISOLATED_PUBACK_P99_MAX:-5ms}
broker_cpu_max_pct=${R3_ISOLATED_BROKER_CPU_MAX_PCT:-75}
broker_cpu_limit_cores=${R3_ISOLATED_BROKER_CPU_LIMIT_CORES:-4}
broker_poll_seconds=${R3_ISOLATED_BROKER_POLL_SECONDS:-5}
topology_grace=${R3_ISOLATED_TOPOLOGY_GRACE:-5s}
run_id=$(date -u +%Y%m%d%H%M%S)-$(printf '%05d' "$RANDOM")
lower_run_id=$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]')
name="nats-r3-tune-$lower_run_id"
name=${name:0:52}
headless="${name}-headless"
client_service="${name}-client"
priority_class="${name}-nonpreempting"
config_map="${name}-config"
credential_secret="${name}-credentials"
tls_secret="${name}-tls"
if [[ $stream_replicas == 1 ]]; then
	stream="FLEET_700K_TEST_${run_id//-/_}"
else
	stream="R3_SHADOW_ISOLATED_${run_id//-/_}"
fi
subject="twitch.outgress.bench.r3.isolated.$lower_run_id"
results_dir=${R3_ISOLATED_RESULTS_DIR:-"/tmp/nats-r3-isolated-${run_id}"}
tmp_dir=$(mktemp -d "/tmp/nats-r3-isolated-material-${run_id}.XXXXXX")
binary="$tmp_dir/nats-live-acceptance"
nodes=(node2 node3 worker1)
publishers=()
publisher_pids=()
topology_pid=
broker_monitor_pid=
cleaned=false
production_baseline="$results_dir/production-baseline.json"
broker_metrics_file="$results_dir/broker-metrics.jsonl"
broker_metrics_stop="$results_dir/broker-metrics.stop"

positive_integer() { [[ $1 =~ ^[1-9][0-9]*$ ]]; }
production_deployments_healthy() {
	kubectl -n production get deployment -o json | jq -e '
		[.items[] |
			(.metadata.generation == (.status.observedGeneration // 0)) and
			((.status.updatedReplicas // 0) == (.spec.replicas // 0)) and
			((.status.readyReplicas // 0) == (.spec.replicas // 0)) and
			((.status.availableReplicas // 0) == (.spec.replicas // 0))] | all
	' >/dev/null
}
production_valkey_healthy() {
	kubectl -n valkey get pods -l app.kubernetes.io/name=valkey -o json | jq -e '
		(.items | length) == 4 and ([.items[] | (.status.containerStatuses | all(.ready == true))] | all)
	' >/dev/null
}
for value in "$seconds" "$fleet_rate" "$min_rate" "$batch" "$outstanding" "$publisher_connections" "$route_pool_size" \
	"$broker_cpu_max_pct" "$broker_cpu_limit_cores" "$broker_poll_seconds"; do
	positive_integer "$value" || { echo "rates, duration, batch, and outstanding values must be positive integers" >&2; exit 1; }
done
case $stream_replicas in
	1|3) ;;
	*) echo "R3_ISOLATED_STREAM_REPLICAS must be 1 or 3" >&2; exit 1 ;;
esac
case $preferred_node in
	node2|node3) ;;
	*) echo "R3_ISOLATED_PREFERRED_NODE must be node2 or node3" >&2; exit 1 ;;
esac
[[ $max_msgs_per_subject == -1 || $max_msgs_per_subject =~ ^[1-9][0-9]*$ ]] || {
	echo "R3_ISOLATED_MAX_MSGS_PER_SUBJECT must be positive or -1" >&2
	exit 1
}
((fleet_rate % ${#nodes[@]} == 0)) || { echo "R3_ISOLATED_RATE must divide evenly across three nodes" >&2; exit 1; }
((min_rate <= fleet_rate)) || { echo "R3_ISOLATED_MIN_RATE must not exceed R3_ISOLATED_RATE" >&2; exit 1; }
case $compression in
	s2_auto)
		compression_config='  compression: { mode: s2_auto, rtt_thresholds: [5ms, 20ms, 50ms] }'
		;;
	s2_fast|s2_better|s2_best)
		compression_config="  compression: { mode: $compression }"
		;;
	off)
		# The NATS config parser treats an unquoted YAML-style `off` as a
		# boolean, but the route compression mode is a string enum.
		compression_config='  compression: { mode: "off" }'
		;;
	*) echo "R3_ISOLATED_COMPRESSION must be s2_auto, s2_fast, s2_better, s2_best, or off" >&2; exit 1 ;;
esac
case $publish_target in
	local|preferred) ;;
	*) echo "R3_ISOLATED_PUBLISH_TARGET must be local or preferred" >&2; exit 1 ;;
esac
case $publish_mode in
	async|atomic|fast) ;;
	*) echo "R3_ISOLATED_PUBLISH_MODE must be async, atomic, or fast" >&2; exit 1 ;;
esac
mkdir "$results_dir"

phase=production-preflight
production_deployments_healthy || {
	echo "production Deployments are not fully rolled out and available; refusing isolated load" >&2
	exit 1
}
production_valkey_healthy || {
	echo "production Valkey is not fully ready; refusing isolated load" >&2
	exit 1
}

kubectl -n production get pods -o json | jq '{
	critical:[.items[] | select(.metadata.name | test("^(nats-[0-2]|twitch-ingress-)") ) |
		{name:.metadata.name,uid:.metadata.uid,restarts:([.status.containerStatuses[]?.restartCount] | add // 0)}]
}' >"$production_baseline"

cleanup() {
	local status=$?
	# macOS ships Bash 3.2, where expanding an empty array under nounset raises
	# an error. Cleanup must also work before the first remote process exists.
	set +u
	if [[ $cleaned == false ]]; then
		cleaned=true
		for pid in "${publisher_pids[@]}" ${topology_pid:-} ${broker_monitor_pid:-}; do
			[[ -n $pid ]] && kill "$pid" >/dev/null 2>&1 || true
		done
		for pid in "${publisher_pids[@]}" ${topology_pid:-} ${broker_monitor_pid:-}; do
			[[ -n $pid ]] && wait "$pid" >/dev/null 2>&1 || true
		done
		kubectl -n "$namespace" delete pod -l "itsbagelbot.dev/r3-tune-run=$run_id" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl -n "$namespace" delete statefulset "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl -n "$namespace" delete service "$client_service" "$headless" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl -n "$namespace" delete configmap "$config_map" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl -n "$namespace" delete secret "$credential_secret" "$tls_secret" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl delete priorityclass "$priority_class" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		rm -rf "$tmp_dir"
	fi
	echo "isolated R3 artifacts retained in $results_dir" >&2
	return "$status"
}
trap cleanup EXIT INT TERM

# NATS substitutes environment values as config tokens. Prefix random hex so a
# leading digit can never be parsed as a numeric quantity instead of a string.
bench_password="b$(openssl rand -hex 24)"
sys_password="s$(openssl rand -hex 24)"
phase=certificate-generation
cat >"$tmp_dir/credentials.env" <<EOF
NATS_USER=bench
NATS_PASSWORD=$bench_password
BENCH_PASSWORD=$bench_password
SYS_PASSWORD=$sys_password
EOF

openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=${name}-ca" \
	-keyout "$tmp_dir/ca.key" -out "$tmp_dir/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -subj "/CN=r3-isolated-tune" \
	-addext "subjectAltName=DNS:$client_service,DNS:$client_service.$namespace,DNS:$client_service.$namespace.svc,DNS:$client_service.$namespace.svc.cluster.local,DNS:$headless,DNS:$headless.$namespace.svc.cluster.local,DNS:*.$headless.$namespace.svc.cluster.local" \
	-keyout "$tmp_dir/tls.key" -out "$tmp_dir/tls.csr" >/dev/null 2>&1
openssl x509 -req -days 2 -in "$tmp_dir/tls.csr" -CA "$tmp_dir/ca.crt" -CAkey "$tmp_dir/ca.key" \
	-CAcreateserial -copy_extensions copy -out "$tmp_dir/tls.crt" >/dev/null 2>&1

phase=resource-creation
cat >"$tmp_dir/nats.conf" <<EOF
server_name: \$POD_NAME
server_tags: [\$POD_NAME]
port: 4222
http_port: 8222
max_connections: 1024
max_payload: 8MB
max_pending: 128MB
# The isolated hub carries streams only. A 10s deadline tolerates a short RAFT
# burst without disconnecting a healthy route; the production RPC leaves keep 2s.
write_deadline: "10s"
ping_interval: "20s"
ping_max: 3
tls {
  cert_file: "/etc/nats/certs/tls.crt"
  key_file: "/etc/nats/certs/tls.key"
  ca_file: "/etc/nats/certs/ca.crt"
  timeout: 5
}
cluster {
  name: "bagelbot-r3-isolated"
  port: 6222
  pool_size: $route_pool_size
  accounts: ["BUS"]
$compression_config
  tls {
    cert_file: "/etc/nats/certs/tls.crt"
    key_file: "/etc/nats/certs/tls.key"
    ca_file: "/etc/nats/certs/ca.crt"
    verify: true
    timeout: 5
  }
  routes: ["nats-route://$headless.$namespace.svc.cluster.local:6222"]
}
jetstream {
  store_dir: /data/jetstream
  domain: isolated
  max_mem: 2GB
  max_file: 512MB
  max_buffered_msgs: 262144
  max_buffered_size: 128MB
}
accounts: {
  BUS: { jetstream: enabled, users: [{user: "bench", password: \$BENCH_PASSWORD}] }
  SYS: { users: [{user: "sys", password: \$SYS_PASSWORD}] }
}
system_account: SYS
max_subscriptions: 0
max_control_line: 4KB
EOF

kubectl apply -f - >/dev/null <<EOF
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: $priority_class
value: -100000
globalDefault: false
preemptionPolicy: Never
description: Temporary non-preempting isolated NATS R3 tuning workload
EOF
kubectl -n "$namespace" create secret generic "$credential_secret" --from-env-file="$tmp_dir/credentials.env" >/dev/null
kubectl -n "$namespace" create secret generic "$tls_secret" \
	--from-file=ca.crt="$tmp_dir/ca.crt" --from-file=tls.crt="$tmp_dir/tls.crt" --from-file=tls.key="$tmp_dir/tls.key" >/dev/null
kubectl -n "$namespace" create configmap "$config_map" --from-file=nats.conf="$tmp_dir/nats.conf" >/dev/null

kubectl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Service
metadata:
  name: $headless
  namespace: $namespace
spec:
  clusterIP: None
  publishNotReadyAddresses: true
  selector:
    itsbagelbot.dev/r3-tune-run: "$run_id"
    app: "$name"
  ports:
    - name: client
      port: 4222
      targetPort: 4222
    - name: cluster
      port: 6222
      targetPort: 6222
---
apiVersion: v1
kind: Service
metadata:
  name: $client_service
  namespace: $namespace
spec:
  selector:
    itsbagelbot.dev/r3-tune-run: "$run_id"
    app: "$name"
  ports:
    - name: client
      port: 4222
      targetPort: 4222
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: $name
  namespace: $namespace
spec:
  serviceName: $headless
  replicas: 3
  podManagementPolicy: Parallel
  selector:
    matchLabels:
      itsbagelbot.dev/r3-tune-run: "$run_id"
      app: "$name"
  template:
    metadata:
      labels:
        itsbagelbot.dev/r3-tune-run: "$run_id"
        app: "$name"
      annotations:
        linkerd.io/inject: "disabled"
    spec:
      priorityClassName: $priority_class
      automountServiceAccountToken: false
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: kubernetes.io/hostname
                    operator: In
                    values:
                      - node2
                      - node3
                      - worker1
      topologySpreadConstraints:
        - maxSkew: 1
          minDomains: 3
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              itsbagelbot.dev/r3-tune-run: "$run_id"
              app: "$name"
      tolerations:
        - key: itsbagelbot.dev/pool
          operator: Equal
          value: worker-pool
          effect: NoSchedule
      containers:
        - name: nats
          image: $image
          args: ["--config", "/etc/nats/nats.conf"]
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
          envFrom:
            - secretRef:
                name: $credential_secret
          ports:
            - name: client
              containerPort: 4222
            - name: monitor
              containerPort: 8222
            - name: cluster
              containerPort: 6222
          readinessProbe:
            httpGet:
              path: "/healthz?js-enabled-only=true"
              port: monitor
            periodSeconds: 2
            failureThreshold: 15
          resources:
            requests:
              cpu: 100m
              memory: 512Mi
            limits:
              cpu: "4"
              memory: 4Gi
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
            readOnlyRootFilesystem: true
          volumeMounts:
            - name: config
              mountPath: /etc/nats
              readOnly: true
            - name: certs
              mountPath: /etc/nats/certs
              readOnly: true
            - name: data
              mountPath: /data/jetstream
      volumes:
        - name: config
          configMap:
            name: $config_map
        - name: certs
          secret:
            secretName: $tls_secret
        - name: data
          emptyDir: {}
EOF

phase=quorum-readiness
kubectl -n "$namespace" rollout status "statefulset/$name" --timeout=180s >/dev/null
placements=()
while IFS= read -r placement; do
	placements+=("$placement")
done < <(kubectl -n "$namespace" get pods -l "itsbagelbot.dev/r3-tune-run=$run_id,app=$name" -o json |
	jq -r '.items[] | [.metadata.name,.spec.nodeName] | @tsv' | sort)
if ((${#placements[@]} != 3)) || [[ $(printf '%s\n' "${placements[@]}" | cut -f2 | sort -u | wc -l | tr -d ' ') != 3 ]]; then
	echo "isolated quorum did not place exactly one broker on each node: ${placements[*]}" >&2
	exit 1
fi
printf '%s\n' "${placements[@]}" | tee "$results_dir/placements.tsv"

broker_metrics_snapshot() {
	local observed_ms broker varz routez jsz sample
	local brokers_json='[]'
	observed_ms=$(($(date +%s) * 1000))
	while IFS= read -r broker; do
		varz=$(kubectl --request-timeout=5s get --raw \
			"/api/v1/namespaces/${namespace}/pods/${broker}:8222/proxy/varz") || return 1
		routez=$(kubectl --request-timeout=5s get --raw \
			"/api/v1/namespaces/${namespace}/pods/${broker}:8222/proxy/routez?subs=0") || return 1
		jsz=$(kubectl --request-timeout=5s get --raw \
			"/api/v1/namespaces/${namespace}/pods/${broker}:8222/proxy/jsz?streams=1") || return 1
		sample=$(jq -nce --arg broker "$broker" --arg stream "$stream" \
			--argjson cpu_limit_cores "$broker_cpu_limit_cores" \
			--argjson varz "$varz" --argjson routez "$routez" --argjson jsz "$jsz" '{
				name:$broker,
				cpu_pct:($varz.cpu // 0),
				cpu_cores:(($varz.cpu // 0) / 100),
				cpu_limit_utilization_pct:(($varz.cpu // 0) / $cpu_limit_cores),
				memory_bytes:($varz.mem // 0),
				connections:($varz.connections // 0),
				slow_consumers:($varz.slow_consumers // 0),
				routes:{
					count:(($routez.routes // []) | length),
					pending_bytes:([($routez.routes // [])[].pending_size] | add // 0),
					max_pending_bytes:([($routez.routes // [])[].pending_size] | max // 0),
					in_msgs:([($routez.routes // [])[].in_msgs] | add // 0),
					out_msgs:([($routez.routes // [])[].out_msgs] | add // 0),
					in_bytes:([($routez.routes // [])[].in_bytes] | add // 0),
					out_bytes:([($routez.routes // [])[].out_bytes] | add // 0)
				},
				jetstream:{
					memory_bytes:($jsz.memory // 0),
					store_bytes:($jsz.store // 0),
					stream:([
						$jsz.account_details[]?.stream_detail[]?
						| select(.name == $stream)
						| {
							leader:(.cluster.leader // ""),
							messages:(.state.messages // 0),
							bytes:(.state.bytes // 0),
							max_follower_lag:([.cluster.replicas[]?.lag] | max // 0),
							all_followers_current:([.cluster.replicas[]?.current] | all)
						}
					] | first // null)
				}
			}') || return 1
		brokers_json=$(jq -c --argjson sample "$sample" '. + [$sample]' <<<"$brokers_json") || return 1
	done < <(printf '%s\n' "${placements[@]}" | cut -f1)
	jq -nc --argjson observed_unix_ms "$observed_ms" --argjson brokers "$brokers_json" \
		'{observed_unix_ms:$observed_unix_ms,brokers:$brokers}'
}

monitor_broker_metrics() {
	local i
	while [[ ! -e $broker_metrics_stop ]]; do
		broker_metrics_snapshot >>"$broker_metrics_file" || return 1
		for ((i = 0; i < broker_poll_seconds; i++)); do
			[[ -e $broker_metrics_stop ]] && return 0
			sleep 1
		done
	done
}

start_broker_monitor() {
	local deadline=$((SECONDS + 30))
	rm -f "$broker_metrics_stop"
	: >"$broker_metrics_file"
	monitor_broker_metrics &
	broker_monitor_pid=$!
	while [[ ! -s $broker_metrics_file ]]; do
		if ! kill -0 "$broker_monitor_pid" 2>/dev/null; then
			echo "isolated broker diagnostics exited before the first sample" >&2
			return 1
		fi
		if ((SECONDS >= deadline)); then
			echo "isolated broker diagnostics did not produce a sample within 30s" >&2
			return 1
		fi
		sleep 0.2
	done
}

stop_broker_monitor() {
	if [[ -z ${broker_monitor_pid:-} ]]; then return 0; fi
	: >"$broker_metrics_stop"
	if ! wait "$broker_monitor_pid"; then
		broker_monitor_pid=
		return 1
	fi
	broker_monitor_pid=
}

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$binary" ./deploy/k8s/nats-live-acceptance
phase=publisher-creation
for node in "${nodes[@]}"; do
	pod="${name}-publisher-${node}"
	publishers+=("$pod")
	broker=$(printf '%s\n' "${placements[@]}" | awk -v node="$node" '$2 == node {print $1}')
	kubectl -n "$namespace" run "$pod" --image=alpine:3.22 --restart=Never --overrides="$(jq -nc \
		--arg node "$node" --arg priority "$priority_class" --arg run "$run_id" \
		--arg credentials "$credential_secret" --arg tls "$tls_secret" --arg broker "$broker" --arg headless "$headless" \
		'{metadata:{labels:{"itsbagelbot.dev/r3-tune-run":$run}},spec:{nodeSelector:{"kubernetes.io/hostname":$node},priorityClassName:$priority,automountServiceAccountToken:false,
		tolerations:[{key:"itsbagelbot.dev/pool",operator:"Equal",value:"worker-pool",effect:"NoSchedule"}],
		containers:[{name:"publisher",image:"alpine:3.22",command:["sleep","1800"],envFrom:[{secretRef:{name:$credentials}}],
		env:[{name:"NATS_CA",value:"/etc/nats-certs/ca.crt"},{name:"R3_BROKER_URL",value:("tls://"+$broker+"."+$headless+":4222")}],
		resources:{requests:{cpu:"100m",memory:"256Mi"},limits:{cpu:"1",memory:"1Gi"}},
		volumeMounts:[{name:"certs",mountPath:"/etc/nats-certs",readOnly:true}]}],volumes:[{name:"certs",secret:{secretName:$tls}}]}}')" >/dev/null
	kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=120s >/dev/null
	kubectl -n "$namespace" cp "$binary" "$pod:/tmp/nats-live-acceptance"
done

control=${publishers[1]}
hub_url="tls://$client_service.$namespace.svc.cluster.local:4222"
worker_server=$(printf '%s\n' "${placements[@]}" | awk '$2 == "worker1" {print $1}')
preferred_server=$(printf '%s\n' "${placements[@]}" | awk -v node="$preferred_node" '$2 == node {print $1}')
placement_tag=
if [[ $stream_replicas == 1 ]]; then
	placement_tag=$preferred_server
fi
phase=stream-creation
kubectl -n "$namespace" exec "$control" -- /tmp/nats-live-acceptance \
	-hub-url="$hub_url" -domain=isolated -stream="$stream" -subject="$subject" \
	-replicas="$stream_replicas" -required-peers="$stream_replicas" -placement-tag="$placement_tag" \
	-max-msgs-per-subject="$max_msgs_per_subject" \
	-create-stream=true -cleanup=false -setup-only=true \
	-settle-timeout=60s >"$results_dir/setup.json"

start_ms=$(( $(date +%s) * 1000 + 10000 ))
per_node_rate=$((fleet_rate / ${#nodes[@]}))
per_node_min=$((min_rate / ${#nodes[@]}))
messages=$((per_node_rate * seconds))
samples=$((seconds * 7))

if [[ $stream_replicas == 3 ]]; then
	kubectl -n "$namespace" exec "$control" -- /tmp/nats-live-acceptance \
		-hub-url="$hub_url" -domain=isolated -stream="$stream" -subject="$subject" \
		-replicas=3 -required-peers=3 -create-stream=false -cleanup=false -topology-only=true \
		-preferred-leader="$preferred_server" -forbidden-leader="$worker_server" \
		-start-at-unix-ms="$start_ms" -topology-duration="${seconds}s" -topology-unhealthy-grace="$topology_grace" \
		-settle-timeout=60s >"$results_dir/topology.json" 2>"$results_dir/topology.err" &
	topology_pid=$!
else
	printf '{"topology":{"passed":true}}\n' >"$results_dir/topology.json"
fi

phase=load
start_broker_monitor
for i in "${!nodes[@]}"; do
	node=${nodes[$i]}
	pod=${publishers[$i]}
	if [[ $publish_target == preferred ]]; then
		broker=$preferred_server
	else
		broker=$(printf '%s\n' "${placements[@]}" | awk -v node="$node" '$2 == node {print $1}')
	fi
	broker_url="tls://$broker.$headless.$namespace.svc.cluster.local:4222"
	kubectl -n "$namespace" exec "$pod" -- /tmp/nats-live-acceptance \
		-hub-url="$broker_url" -domain=isolated -stream="$stream" -subject="$subject" \
		-replicas="$stream_replicas" -required-peers="$stream_replicas" -create-stream=false -cleanup=false \
		-mode="$publish_mode" -batch-size="$batch" -fast-outstanding-acks="$outstanding" \
		-target-rate="$per_node_rate" -min-rate="$per_node_min" -messages="$messages" \
		-publishers="$publisher_connections" -window=16384 -payload-bytes=256 -payload-variants=65536 \
		-latency-samples="$samples" -latency-interval=143ms -max-p95="$puback_p99" -max-p99="$puback_p99" \
		-ack-timeout=5s -run-timeout="$((seconds + 30))s" -max-ack-gap=2s \
		-start-at-unix-ms="$start_ms" -producer-id="isolated-$run_id-$node" \
		>"$results_dir/$node.json" 2>"$results_dir/$node.err" &
	publisher_pids+=("$!")
done

status=0
while :; do
	running=false
	for pid in "${publisher_pids[@]}"; do
		if kill -0 "$pid" 2>/dev/null; then running=true; fi
	done
	[[ $running == false ]] && break
	if ! production_deployments_healthy; then
		echo "production Deployment availability changed during isolated test" >&2
		status=1
		break
	fi
	if ! kubectl -n production get pods -o json | jq -e --slurpfile baseline "$production_baseline" '
		[.items[] | select(.metadata.name | test("^(nats-[0-2]|twitch-ingress-)")) |
			{name:.metadata.name,uid:.metadata.uid,restarts:([.status.containerStatuses[]?.restartCount] | add // 0)}] as $now |
		($now | sort_by(.name)) == ($baseline[0].critical | sort_by(.name)) and
		(([.items[] | select(.metadata.name | test("^nats-[0-2]$")) |
			(.status.containerStatuses | all(.ready == true))]) as $ready |
			($ready | length) == 3 and ($ready | all))
	' >/dev/null; then
		echo "production NATS/ingress identity, restart, or readiness changed during isolated test" >&2
		status=1
		break
	fi
	if ! production_valkey_healthy; then
		echo "production Valkey readiness changed during isolated test" >&2
		status=1
		break
	fi
	sleep 5
done
if ((status != 0)); then
	for pid in "${publisher_pids[@]}"; do kill "$pid" >/dev/null 2>&1 || true; done
	kubectl -n "$namespace" delete pod "${publishers[@]}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
fi
for pid in "${publisher_pids[@]}"; do wait "$pid" || status=1; done
if [[ -n ${topology_pid:-} ]]; then
	wait "$topology_pid" || status=1
fi
stop_broker_monitor || status=1
publisher_pids=()
topology_pid=

for broker in $(printf '%s\n' "${placements[@]}" | cut -f1); do
	kubectl -n "$namespace" logs "$broker" >"$results_dir/$broker.log"
done
phase=result-validation
if grep -Eina 'IPQ len limit|slow consumer|write deadline( exceeded)?|no quorum|quorum (lost|unavailable)' "$results_dir"/${name}-*.log >"$results_dir/forbidden-nats.log"; then
	status=1
fi

jq -s --slurpfile topology "$results_dir/topology.json" \
	--slurpfile metrics "$broker_metrics_file" \
	--arg compression "$compression" --arg publishTarget "$publish_target" \
	--arg publishMode "$publish_mode" \
	--argjson publishers "$publisher_connections" \
	--argjson replicas "$stream_replicas" \
	--argjson routePoolSize "$route_pool_size" \
	--arg preferredNode "$preferred_node" \
	--argjson maxMsgsPerSubject "$max_msgs_per_subject" \
	--argjson target "$fleet_rate" --argjson minimum "$min_rate" \
	--argjson brokerCpuMaxPct "$broker_cpu_max_pct" '
	[.[].results[0]] as $r |
	($r|map(.started_unix_ms)|min) as $start |
	($r|map(.finished_unix_ms)|max) as $finish |
	($r|map(.acknowledged)|add) as $acked |
	([$metrics[] |
		select(.observed_unix_ms >= $start and .observed_unix_ms <= $finish) |
		.brokers[]]) as $brokerSamples |
	($brokerSamples | group_by(.name) | map({
		name:.[0].name,
		samples:length,
		max_cpu_cores:(map(.cpu_cores) | max),
		max_cpu_limit_utilization_pct:(map(.cpu_limit_utilization_pct) | max),
		max_memory_bytes:(map(.memory_bytes) | max),
		max_route_pending_bytes:(map(.routes.max_pending_bytes) | max),
		max_total_route_pending_bytes:(map(.routes.pending_bytes) | max),
		max_follower_lag:(map(.jetstream.stream.max_follower_lag // 0) | max)
	})) as $brokerMetrics |
	{
	  route_compression:$compression,
	  publish_target:$publishTarget,
	  publish_mode:$publishMode,
	  publishers_per_node:$publishers,
	  stream_replicas:$replicas,
	  route_pool_size:$routePoolSize,
	  preferred_node:$preferredNode,
	  max_msgs_per_subject:$maxMsgsPerSubject,
	  target_messages_per_second:$target,
	  minimum_messages_per_second:$minimum,
	  acknowledged:$acked,
	  conservative_duration_ms:($finish-$start),
	  aggregate_messages_per_second:($acked/(($finish-$start)/1000)),
	  puback_min_ms:($r|map(.puback_min_ms)|min),
	  worst_node_puback_p50_ms:($r|map(.puback_p50_ms)|max),
	  worst_node_puback_p95_ms:($r|map(.puback_p95_ms)|max),
	  worst_node_puback_p99_ms:($r|map(.puback_p99_ms)|max),
	  puback_max_ms:($r|map(.puback_max_ms)|max),
	  errors:($r|map(.errors)|add),
	  timeouts:($r|map(.timeouts)|add),
	  broker_metrics:{
		gate_cpu_pct:$brokerCpuMaxPct,
		sample_count:($brokerSamples | length),
		peak_cpu_cores:([$brokerSamples[].cpu_cores] | max // null),
		peak_cpu_limit_utilization_pct:([$brokerSamples[].cpu_limit_utilization_pct] | max // null),
		peak_memory_bytes:([$brokerSamples[].memory_bytes] | max // null),
		peak_route_pending_bytes:([$brokerSamples[].routes.max_pending_bytes] | max // null),
		peak_follower_lag:([$brokerSamples[].jetstream.stream.max_follower_lag // 0] | max // null),
		brokers:$brokerMetrics
	  },
	  topology:$topology[0].topology,
	  nodes:$r,
	  passed:(
		($r|all(.passed == true)) and
		$topology[0].topology.passed == true and
		($acked/(($finish-$start)/1000)) >= $minimum and
		($brokerSamples | length) >= 3 and
		([$brokerSamples[].cpu_limit_utilization_pct] | max) <= $brokerCpuMaxPct
	  )
	}' "$results_dir"/node2.json "$results_dir"/node3.json "$results_dir"/worker1.json >"$results_dir/summary.json" || status=1

kubectl -n "$namespace" exec "$control" -- /tmp/nats-live-acceptance \
	-hub-url="$hub_url" -domain=isolated -stream="$stream" -subject="$subject" \
	-replicas="$stream_replicas" -required-peers="$stream_replicas" -placement-tag="$placement_tag" \
	-create-stream=false -cleanup=true -setup-only=true \
	-settle-timeout=60s >"$results_dir/cleanup.json" || status=1

jq . "$results_dir/summary.json" || true
if ((status != 0)) || ! jq -e '.passed == true' "$results_dir/summary.json" >/dev/null 2>&1; then
	echo "isolated R3 qualification failed" >&2
	exit 1
fi
echo "isolated R3 qualification passed at ${fleet_rate} events/s for ${seconds}s"
