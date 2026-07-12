#!/usr/bin/env bash
# Proves cross-account RPC takes the direct leaf-cluster route and never the hub.
set -euo pipefail

namespace=${NAMESPACE:-production}
request_node=${REQUEST_NODE:-node3}
response_node=${RESPONSE_NODE:-node2}
messages=${MESSAGES:-100000}
clients=${CLIENTS:-8}
image=${NATS_BOX_IMAGE:-natsio/nats-box:0.17.0}
run_id=$(date -u +%Y%m%d%H%M%S)
request_pod="nats-rpc-request-${run_id}"
response_pod="nats-rpc-response-${run_id}"
subject="bagel.rpc.admin.user.bench.leafdirect${run_id}"
responder_pid=""

cleanup() {
  if [[ -n "$responder_pid" ]]; then
    kill "$responder_pid" 2>/dev/null || true
  fi
  kubectl -n "$namespace" delete pod "$request_pod" "$response_pod" \
    --ignore-not-found --wait=false >/dev/null
}
trap cleanup EXIT INT TERM

create_client_pod() {
  local pod=$1 node=$2 secret=$3
  local overrides
  overrides=$(jq -nc \
    --arg pod "$pod" \
    --arg node "$node" \
    --arg secret "$secret" \
    --arg image "$image" \
    '{spec:{
      nodeName:$node,
      restartPolicy:"Never",
      containers:[{
        name:$pod,
        image:$image,
        command:["sleep","900"],
        env:[
          {name:"NATS_USER",valueFrom:{secretKeyRef:{name:$secret,key:"NATS_RPC_USER"}}},
          {name:"NATS_PASSWORD",valueFrom:{secretKeyRef:{name:$secret,key:"NATS_RPC_PASSWORD"}}}
        ],
        volumeMounts:[{name:"fleet-ca",mountPath:"/etc/nats-ca",readOnly:true}],
        resources:{requests:{cpu:"100m",memory:"64Mi"},limits:{cpu:"1",memory:"256Mi"}},
        securityContext:{runAsUser:1000,runAsGroup:1000,allowPrivilegeEscalation:false,capabilities:{drop:["ALL"]}}
      }],
      volumes:[{name:"fleet-ca",configMap:{name:"fleet-ca"}}],
      securityContext:{runAsNonRoot:true,runAsUser:1000,runAsGroup:1000,seccompProfile:{type:"RuntimeDefault"}}
    }}')
  kubectl -n "$namespace" run "$pod" --image="$image" --restart=Never \
    --overrides="$overrides" -- sleep 900 >/dev/null
}

leaf_pod_on() {
  kubectl -n "$namespace" get pod -l app=nats-leaf \
    --field-selector="spec.nodeName=$1" \
    -o jsonpath='{.items[0].metadata.name}'
}

monitor() {
  kubectl -n "$namespace" exec "$1" -c nats -- wget -qO- "http://127.0.0.1:8222/$2"
}

route_total() {
  monitor "$1" 'routez?subs=0' |
    jq --arg ip "$2" '[.routes[]|select(.ip==$ip)|(.in_msgs+.out_msgs)]|add//0'
}

rpc_hub_total() {
  monitor "$1" 'leafz?subs=0' |
    jq '[.leafs[]|select(.account=="ADMIN_RPC" or .account=="USERS_RPC")|(.in_msgs+.out_msgs)]|add//0'
}

create_client_pod "$response_pod" "$response_node" users-env
create_client_pod "$request_pod" "$request_node" console-admin-env
kubectl -n "$namespace" wait --for=condition=Ready \
  "pod/$response_pod" "pod/$request_pod" --timeout=90s >/dev/null

response_leaf=$(leaf_pod_on "$response_node")
request_leaf=$(leaf_pod_on "$request_node")
response_leaf_ip=$(kubectl -n "$namespace" get pod "$response_leaf" -o jsonpath='{.status.podIP}')

before_route=$(route_total "$request_leaf" "$response_leaf_ip")
before_hub=$(rpc_hub_total "$request_leaf")

kubectl -n "$namespace" exec "$response_pod" -- sh -c \
  "NATS_CA=/etc/nats-ca/ca.pem nats --no-context \
   --connection-name leaf-direct-rpc-responder \
   -s tls://nats-leaf-local:4222 bench service serve \
   --msgs '$messages' --clients 4 --no-progress --size 128 '$subject'" \
  >/tmp/nats-leaf-rpc-responder.log 2>&1 &
responder_pid=$!

for _ in $(seq 1 30); do
  ready=$(monitor "$response_leaf" 'connz?subs=detail' |
    jq '[.connections[]|select((.name//"")=="leaf-direct-rpc-responder" and .tls_version!="")]|length')
  [[ "$ready" -ge 1 ]] && break
  sleep 0.2
done
if [[ ${ready:-0} -lt 1 ]]; then
  echo 'responder did not establish a TLS connection to its node-local leaf' >&2
  exit 1
fi

kubectl -n "$namespace" exec "$request_pod" -- sh -c \
  "NATS_CA=/etc/nats-ca/ca.pem nats --no-context \
   --connection-name leaf-direct-rpc-requester \
   -s tls://nats-leaf-local:4222 bench service request \
   --msgs '$messages' --clients '$clients' --no-progress --size 128 '$subject'"

after_route=$(route_total "$request_leaf" "$response_leaf_ip")
after_hub=$(rpc_hub_total "$request_leaf")
route_delta=$((after_route - before_route))
hub_delta=$((after_hub - before_hub))

jq -nc \
  --arg request_node "$request_node" \
  --arg response_node "$response_node" \
  --argjson messages "$messages" \
  --argjson route_messages "$route_delta" \
  --argjson rpc_hub_messages "$hub_delta" \
  '{request_node:$request_node,response_node:$response_node,messages:$messages,
    direct_leaf_route_messages:$route_messages,rpc_hub_messages:$rpc_hub_messages,
    passed:($route_messages >= ($messages*2) and $rpc_hub_messages == 0)}'

if (( route_delta < messages * 2 )); then
  echo "direct leaf route carried only $route_delta messages; expected request+reply for $messages RPCs" >&2
  exit 1
fi
if (( hub_delta != 0 )); then
  echo "RPC hub bridge carried $hub_delta messages; RPC is not hub-independent" >&2
  exit 1
fi
