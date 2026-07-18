# Local-first NATS gateway acceptance

This opt-in lane proves the topology needed for strict local-first queue RPC
without changing the production NATS hub, leaf DaemonSet, streams, or client
configuration. It creates a temporary native-TLS supercluster:

- `rpc-core`: two routed servers, pinned to node2 and node3;
- `rpc-worker1`: one edge server pinned to worker1;
- `rpc-node1`: one edge server pinned to node1.

This follows NATS' documented [gateway queue behavior](https://docs.nats.io/running-a-nats-service/configuration/gateways):
a server selects a queue member in its own cluster and forwards to another
cluster only when local interest is absent. Among eligible remote clusters,
NATS selects the lowest-RTT cluster.

All four servers reuse the production account/import/export configuration and
the existing RPC users. cert-manager issues a 24-hour server/client certificate
from `fleet-ca-issuer`, following the current NATS PKI pattern. The certificate
contains only the temporary Service names and gateway/route SAN verification is
enabled. The topology does not enable JetStream or leaf nodes.

The acceptance NetworkPolicy allows its pods to reach only each other and
CoreDNS. Consequently the temporary clients and servers cannot fall through to
the production hub or leaf tier.

Without the exact confirmation token the runner prints its plan and exits:

```sh
deploy/k8s/nats-live-acceptance/gateway/local-first.sh
```

Run the live-hardware proof in a controlled, quiet window:

```sh
CONFIRM_NATS_GATEWAY_ACCEPTANCE=LOCAL-FIRST-RPC \
  deploy/k8s/nats-live-acceptance/gateway/local-first.sh
```

No local binary or container image is built. The runner uses the existing
multi-architecture `nats:2.14.3-alpine` and `natsio/nats-box:0.17.0` images on
the cluster. Temporary resources are deleted on success, interruption, or
failure. It refuses to adopt a prior run's resources and verifies that the
production NATS/leaf pod identities and restart counts remain unchanged.

For each cluster, the runner:

1. starts one queue responder in the requester's cluster and one in a remote
   cluster, then requires every response to come from the local responder;
2. removes the local responder and requires every request to use the remote
   responder;
3. measures local and remote-fallback request/reply tails and applies the same
   p99 ceiling used by production: 8 ms, never a relaxed value;
4. verifies native TLS on RPC client connections and that JetStream is absent.

The default correctness sample is 500 requests per phase. Latency phases use
5,000 requests and four clients. `CORRECTNESS_REQUESTS`, `LATENCY_REQUESTS`, and
`LATENCY_CLIENTS` may increase coverage; `RPC_P99_MAX_MS` may only tighten the
8 ms gate.

## Migration caveats

- Local-first behavior applies to queue subscriptions. Ordinary subscriptions
  still fan out across the supercluster and must not be used for RPC workers.
- A production migration needs stable per-cluster names and certificates whose
  SANs match every advertised route/gateway address. Each cluster must carry an
  identical account/import/export definition.
- Keep JetStream on the current hub. RPC gateway servers are core-NATS-only and
  should not receive BUS or `$JS.API` permissions.
- Do not run the gateway and the routed leaf tier as two paths for the same RPC
  account during migration; parallel interest paths can duplicate or bypass the
  locality guarantee. Move one canary account/service at a time.
- worker1 and node1 are single-server edge clusters. They prefer same-cluster
  responders while healthy and fall back remotely, but they do not provide
  local RPC availability during their own server restart. That trade-off should
  be handled with fast client reconnect and a staged rolling procedure.
- The 8 ms fallback gate is intentionally strict. A worker1-to-core fallback
  failure is evidence that the WAN path cannot satisfy the historical RPC SLA;
  it is not a reason to loosen the ceiling.
