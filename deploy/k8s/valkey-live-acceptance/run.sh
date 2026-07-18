#!/usr/bin/env bash
# Fleet-wide production Valkey p99 acceptance. Temporary probe pods are removed
# even when a node misses the target.
set -euo pipefail

namespace=${NAMESPACE:-valkey}
nodes=${NODES:-"node1 node2 node3 worker1"}
requests=${REQUESTS:-100000}
warmup=${WARMUP:-5000}
concurrency=${CONCURRENCY:-5}
mode=${MODE:-both}
write_profile=${WRITE_PROFILE:-pooled}
target=${TARGET:-2ms}
require_target=${REQUIRE_TARGET:-false}
image=${BENCH_IMAGE:-}
cpu_request=${CPU_REQUEST:-50m}
pod_timeout_seconds=${POD_TIMEOUT_SECONDS:-420}
cleanup_timeout_seconds=${CLEANUP_TIMEOUT_SECONDS:-30}
priority_class=${BENCH_PRIORITY_CLASS:-valkey-bench-nonpreempting}
run_id=$(date -u +%Y%m%d%H%M%S)
declare -a pods=()

cleanup() {
  # The default expansion keeps cleanup safe under Bash 3.2 + nounset before
  # the first pod has been appended (for example when a safety gate rejects).
  for pod in "${pods[@]-}"; do
    [[ -n $pod ]] || continue
    kubectl -n "$namespace" delete pod "$pod" --ignore-not-found --wait=true \
      --timeout="${cleanup_timeout_seconds}s" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT INT TERM

if [[ ${CONFIRM_LIVE_VALKEY_BENCH:-} != "I-understand-this-runs-on-live-Valkey" ]]; then
  echo "Refusing live Valkey benchmark without explicit confirmation." >&2
  echo "Set CONFIRM_LIVE_VALKEY_BENCH=I-understand-this-runs-on-live-Valkey" >&2
  exit 1
fi

if [[ ! $image =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]]; then
  echo "BENCH_IMAGE must identify one immutable image digest (repository@sha256:<64 lowercase hex characters>)" >&2
  exit 1
fi

if [[ $require_target != true && $require_target != false ]]; then
  echo "REQUIRE_TARGET must be true or false" >&2
  exit 1
fi
if [[ ! $pod_timeout_seconds =~ ^[1-9][0-9]*$ || ! $cleanup_timeout_seconds =~ ^[1-9][0-9]*$ ]]; then
  echo "POD_TIMEOUT_SECONDS and CLEANUP_TIMEOUT_SECONDS must be positive integers" >&2
  exit 1
fi

priority_json=$(kubectl get priorityclass "$priority_class" -o json)
if ! jq -e '.value <= 0 and .preemptionPolicy == "Never"' <<<"$priority_json" >/dev/null; then
  echo "Valkey benchmark priority class $priority_class must have value <= 0 and preemptionPolicy Never" >&2
  exit 1
fi

wait_for_termination() {
  local pod=$1
  local deadline=$((SECONDS + pod_timeout_seconds))
  local exit_code
  local wait_reason

  while ((SECONDS < deadline)); do
    exit_code=$(kubectl -n "$namespace" get pod "$pod" \
      -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}' 2>/dev/null || true)
    if [[ $exit_code =~ ^[0-9]+$ ]]; then
      return 0
    fi

    wait_reason=$(kubectl -n "$namespace" get pod "$pod" \
      -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)
    case "$wait_reason" in
      CreateContainerConfigError|CreateContainerError|ErrImagePull|ImagePullBackOff|InvalidImageName)
        return 1
        ;;
    esac
    sleep 1
  done
  return 1
}

status=0
for node in $nodes; do
  pod="valkey-p99-${node}-${run_id}"
  pods+=("$pod")

  args=(
    -node "$node" -mode "$mode" -requests "$requests" -warmup "$warmup"
    -concurrency "$concurrency" -target "$target" -write-profile "$write_profile"
  )
  if [[ $require_target == true ]]; then
    args+=(-require-target)
  fi
  args_json=$(jq -nc '$ARGS.positional' --args -- "${args[@]}")

  overrides=$(jq -nc --arg pod "$pod" --arg node "$node" --arg image "$image" \
    --arg cpuRequest "$cpu_request" --arg priorityClass "$priority_class" \
    --argjson args "$args_json" '{
    spec:{nodeName:$node,restartPolicy:"Never",priorityClassName:$priorityClass,
    automountServiceAccountToken:false,containers:[{
      name:$pod,image:$image,imagePullPolicy:"Always",args:$args,
      env:[
        {name:"NODE_NAME",valueFrom:{fieldRef:{fieldPath:"spec.nodeName"}}},
        {name:"NODE_IP",valueFrom:{fieldRef:{fieldPath:"status.hostIP"}}},
        {name:"VALKEY_LOCAL_ADDR",value:"valkey-local.valkey.svc.cluster.local:6380"},
        {name:"REDISCLI_AUTH",valueFrom:{secretKeyRef:{name:"valkey-core-secret",key:"valkey-password"}}}
      ],
      volumeMounts:[{name:"tls",mountPath:"/etc/valkey/tls",readOnly:true}],
      resources:{requests:{cpu:$cpuRequest,memory:"128Mi"},limits:{cpu:"2",memory:"768Mi"}},
      securityContext:{runAsUser:65532,runAsGroup:65532,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
    }],volumes:[{name:"tls",secret:{secretName:"valkey-server-tls"}}],
    securityContext:{runAsNonRoot:true,runAsUser:65532,runAsGroup:65532,seccompProfile:{type:"RuntimeDefault"}}
  }}')

  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never --overrides="$overrides" >/dev/null
  if ! wait_for_termination "$pod"; then
    kubectl -n "$namespace" logs "$pod" || true
    kubectl -n "$namespace" get pod "$pod" -o wide || true
    status=1
    kubectl -n "$namespace" delete pod "$pod" --wait=true \
      --timeout="${cleanup_timeout_seconds}s" >/dev/null 2>&1 || true
    continue
  fi

  if ! kubectl -n "$namespace" logs "$pod"; then
    status=1
  fi
  exit_code=$(kubectl -n "$namespace" get pod "$pod" \
    -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}')
  if [[ $exit_code != 0 ]]; then
    status=1
  fi
  kubectl -n "$namespace" delete pod "$pod" --wait=true \
    --timeout="${cleanup_timeout_seconds}s" >/dev/null 2>&1 || true
done

exit "$status"
