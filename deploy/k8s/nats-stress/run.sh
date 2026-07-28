#!/usr/bin/env bash
# nats-stress runner: ceiling + stable rate for the rewritten NATS bus, and the
# idempotency guard validated at that rate.
#
# BUILDING AND RUNNING ARE SEPARATE STEPS ON PURPOSE. This script never builds
# and never pushes; it takes an image digest that already exists and puts load on
# the LIVE broker with it. Build it yourself first, from the repository root:
#
#   podman build -t ghcr.io/adamousmer/itsbagelbot/nats-stress:local \
#     -f deploy/k8s/nats-stress/Containerfile .
#   podman push ...                     # then resolve the pushed digest
#
# or let .github/workflows/publish-images.yml build it and take the digest from
# there. Never docker; this fleet is podman.
#
# Everything it creates is removed on exit, including the shadow stream.
set -euo pipefail

namespace=${NAMESPACE:-production}
image=${BENCH_IMAGE:-}
publisher_replicas=${PUBLISHER_REPLICAS:-2}
consumer_replicas=${CONSUMER_REPLICAS:-1}
lanes=${LANES:-8}
payload_bytes=${PAYLOAD_BYTES:-512}
start_rate=${START_RATE:-20000}
step_rate=${STEP_RATE:-10000}
step_hold=${STEP_HOLD:-30s}
max_steps=${MAX_STEPS:-20}
soak=${SOAK:-5m}
# Atomic cohort shape under test; substituted into publisher.yaml so a tuning
# sweep needs no image rebuild. Defaults mirror the bus defaults.
atomic_batch_size=${ATOMIC_BATCH_SIZE:-256}
atomic_batch_wait=${ATOMIC_BATCH_WAIT:-1ms}
atomic_overlap=${ATOMIC_OVERLAP:-on}
dup_fraction=${DUP_FRACTION:-0.01}
routines=${ROUTINES:-100}
guard_ttl=${GUARD_TTL:-2m}
ready_timeout=${READY_TIMEOUT_SECONDS:-300}
run_timeout=${RUN_TIMEOUT_SECONDS:-3600}
priority_class=${BENCH_PRIORITY_CLASS:-nats-r3-bench-nonpreempting}

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd -- "$here/../../.." && pwd)
run_id=$(date -u +%Y%m%d%H%M%S)
# WORKDIR keeps a run's raw JSONL after teardown: the merged verdict cannot
# explain a consumer that under-delivers, only the per-role lines can.
workdir=${WORKDIR:-$(mktemp -d)}
reporter="$workdir/nats-stress-report"
publisher_log="$workdir/publisher.jsonl"
consumer_log="$workdir/consumer.jsonl"
declare -a tails=()

banner() {
  cat >&2 <<'BANNER'
================================================================================
  nats-stress: LIVE LOAD GENERATOR

  This drives the production NATS hub to its ceiling and claims idempotency
  keys on the production Valkey primary. It writes only to its own shadow
  stream (R3_SHADOW_BENCH) and its own subject tree, and it runs at a
  non-preempting negative priority so it yields to every real workload -- but
  the broker CPU, the RAFT commit path and the network it saturates are the
  ones production is using at the same moment.

  Do not run this during an incident, a rollout, or a stream.
================================================================================
BANNER
}

cleanup() {
  for pid in "${tails[@]-}"; do
    [[ -n $pid ]] || continue
    kill "$pid" >/dev/null 2>&1 || true
  done
  kubectl -n "$namespace" delete deployment nats-stress-publisher nats-stress-consumer \
    --ignore-not-found --wait=true --timeout=90s >/dev/null 2>&1 || true
  teardown_stream || true
  # An operator-supplied WORKDIR is theirs to keep; only a temp one is ours.
  [[ -n ${WORKDIR:-} ]] || rm -rf "$workdir"
}

# teardown_stream runs the rig's own delete role in a one-shot pod carrying the
# admin credential. It deliberately does not reuse a consumer pod: by the time
# teardown happens the consumer Deployment is gone, and a teardown that depends
# on the thing it is cleaning up after is not a teardown.
teardown_stream() {
  local pod="nats-stress-teardown-${run_id}"
  local overrides
  overrides=$(jq -nc --arg pod "$pod" --arg image "$image" --arg priority "$priority_class" '{
    spec:{restartPolicy:"Never",priorityClassName:$priority,automountServiceAccountToken:false,
    tolerations:[{key:"itsbagelbot.dev/pool",operator:"Equal",value:"worker-pool",effect:"NoSchedule"}],
    containers:[{name:$pod,image:$image,args:["-role","teardown"],
      env:[
        {name:"NATS_HUB_URL",value:"tls://nats-0.nats-headless.production.svc.cluster.local:4222"},
        {name:"NATS_CA_PEM",valueFrom:{configMapKeyRef:{name:"fleet-ca",key:"ca.pem"}}},
        {name:"NATS_USER",valueFrom:{secretKeyRef:{name:"nats-bench-setup",key:"NATS_USER"}}},
        {name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:"nats-bench-setup",key:"NATS_PASSWORD"}}}
      ],
      resources:{requests:{cpu:"50m",memory:"64Mi"},limits:{cpu:"500m",memory:"256Mi"}},
      securityContext:{allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]},readOnlyRootFilesystem:true}}],
    securityContext:{runAsNonRoot:true,runAsUser:65532,runAsGroup:65532,seccompProfile:{type:"RuntimeDefault"}}}
  }')
  # Args come from the overrides only. Passing them again after -- would put the
  # flag on the command line twice, which the flag package tolerates and a reader
  # does not.
  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never \
    --overrides="$overrides" >/dev/null 2>&1 || return 1
  kubectl -n "$namespace" wait --for=jsonpath='{.status.phase}'=Succeeded "pod/$pod" \
    --timeout=120s >/dev/null 2>&1 || kubectl -n "$namespace" logs "$pod" >&2 || true
  kubectl -n "$namespace" delete pod "$pod" --ignore-not-found --wait=false >/dev/null 2>&1 || true
}

require_confirmation() {
  banner
  if [[ ${CONFIRM_LIVE_NATS_STRESS:-} != "I-understand-this-loads-live-NATS" ]]; then
    echo "Refusing to run. Set CONFIRM_LIVE_NATS_STRESS=I-understand-this-loads-live-NATS" >&2
    exit 1
  fi
}

# require_image insists on an immutable digest. A moving tag means the run cannot
# be attributed to a commit, and a benchmark nobody can attribute is a rumour.
require_image() {
  if [[ ! $image =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]]; then
    echo "BENCH_IMAGE must be one immutable digest (repository@sha256:<64 lowercase hex>)" >&2
    exit 1
  fi
}

# require_go finds the toolchain. It is not on PATH on the fleet workstation, and
# the report merge is a local build: without this the run reaches the end and
# then cannot judge itself.
require_go() {
  if command -v go >/dev/null 2>&1; then
    return
  fi
  if [[ -x "$HOME/sdk/go1.26.0/bin/go" ]]; then
    PATH="$HOME/sdk/go1.26.0/bin:$PATH"
    export PATH
    return
  fi
  echo "go toolchain not found (tried PATH and \$HOME/sdk/go1.26.0/bin)" >&2
  exit 1
}

require_priority_class() {
  local json
  json=$(kubectl get priorityclass "$priority_class" -o json)
  if ! jq -e '.value <= 0 and .preemptionPolicy == "Never"' <<<"$json" >/dev/null; then
    echo "Priority class $priority_class must have value <= 0 and preemptionPolicy Never" >&2
    exit 1
  fi
}

require_secrets() {
  local name
  for name in nats-bench-setup sesame-env; do
    if ! kubectl -n "$namespace" get secret "$name" >/dev/null 2>&1; then
      echo "Missing Secret $name in namespace $namespace" >&2
      exit 1
    fi
  done
  if ! kubectl -n "$namespace" get configmap fleet-ca >/dev/null 2>&1; then
    echo "Missing ConfigMap fleet-ca in namespace $namespace" >&2
    exit 1
  fi
}

json_args() {
  jq -nc '$ARGS.positional' --args -- "$@"
}

apply_manifest() {
  local file=$1 args=$2 replicas=$3 token=$4
  sed -e "s|__BENCH_IMAGE__|$image|g" \
      -e "s|__${token}_ARGS__|$args|g" \
      -e "s|__${token}_REPLICAS__|$replicas|g" \
      "$file" | kubectl -n "$namespace" apply -f - >/dev/null
}

follow_logs() {
  local role=$1 destination=$2
  kubectl -n "$namespace" logs -f -l "app=nats-stress,role=$role" \
    --max-log-requests=10 --tail=-1 >"$destination" 2>/dev/null &
  tails+=("$!")
}

# wait_for_line polls a captured log for a marker. Polling the file rather than
# piping through grep keeps the full capture on disk: the merge needs every
# summary line, and a pipeline that consumed the stream to find one marker would
# have thrown the rest away.
wait_for_line() {
  # Two statements: local expands every word before it assigns any of them,
  # so deadline's arithmetic would read timeout while it is still unset (-u).
  local file=$1 marker=$2 timeout=$3
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if grep -q "$marker" "$file" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

start_consumer() {
  local args
  args=$(json_args -role consume -routines "$routines" -guard-ttl "$guard_ttl" -tick 5s)
  apply_manifest "$here/consumer.yaml" "$args" "$consumer_replicas" CONSUMER
  kubectl -n "$namespace" rollout status deployment/nats-stress-consumer \
    --timeout="${ready_timeout}s" >/dev/null
  follow_logs consumer "$consumer_log"
  # The consumer holds the only credential that may create the shadow stream, so
  # nothing may publish until it says the stream exists. Starting the publishers
  # first would make their whole first step measure the broker's rejection path.
  if ! wait_for_line "$consumer_log" '"kind":"stream_ready"' "$ready_timeout"; then
    echo "Consumer never reported the shadow stream ready" >&2
    exit 1
  fi
}

start_publisher() {
  local args
  args=$(json_args -role publish \
    -lanes "$lanes" -payload-bytes "$payload_bytes" -dup-fraction "$dup_fraction" \
    -start-rate "$start_rate" -step-rate "$step_rate" -step-hold "$step_hold" \
    -max-steps "$max_steps" -soak "$soak" -tick 5s)
  sed -e "s|__ATOMIC_BATCH_SIZE__|$atomic_batch_size|g" \
      -e "s|__ATOMIC_BATCH_WAIT__|$atomic_batch_wait|g" \
      -e "s|__ATOMIC_OVERLAP__|$atomic_overlap|g" \
      "$here/publisher.yaml" >"$workdir/publisher.rendered.yaml"
  apply_manifest "$workdir/publisher.rendered.yaml" "$args" "$publisher_replicas" PUBLISHER
  kubectl -n "$namespace" rollout status deployment/nats-stress-publisher \
    --timeout="${ready_timeout}s" >/dev/null
  follow_logs publisher "$publisher_log"
}

# collect_consumer_summary deletes the consumer so it receives SIGTERM, which is
# what makes it flush its final summary. The follower is still attached, so the
# summary lands in the capture before the pod goes away.
collect_consumer_summary() {
  kubectl -n "$namespace" delete deployment nats-stress-consumer \
    --ignore-not-found --wait=true --timeout=90s >/dev/null
  wait_for_line "$consumer_log" '"role":"consume","kind":"summary"' 120 || true
}

main() {
  require_confirmation
  require_image
  require_go
  require_priority_class
  require_secrets

  trap cleanup EXIT INT TERM
  ( cd "$repo" && go build -trimpath -o "$reporter" ./deploy/k8s/nats-stress )

  start_consumer
  start_publisher

  echo "nats-stress running; logs collecting under $workdir" >&2
  if ! wait_for_line "$publisher_log" '"role":"publish","kind":"summary"' "$run_timeout"; then
    echo "Publishers never reported a summary within ${run_timeout}s" >&2
    exit 1
  fi
  kubectl -n "$namespace" delete deployment nats-stress-publisher \
    --ignore-not-found --wait=true --timeout=90s >/dev/null
  collect_consumer_summary

  echo "--- verdict ---" >&2
  "$reporter" -role report "$publisher_log" "$consumer_log"
}

main "$@"
