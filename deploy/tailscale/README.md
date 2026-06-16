# Tailnet plumbing

Policy-as-code for the tailnet plus the Tailscale Kubernetes operator
manifests. The end state: bare metal accepts only SSH from operator
devices; everything k3s-hosted is reached through operator-managed
proxies under `*.tail451e6d.ts.net`.

| File                    | What it is                                              |
|-------------------------|---------------------------------------------------------|
| `policy.hujson`         | Final tailnet ACL policy (lockdown).                    |
| `policy.cutover.hujson` | Same plus a marked temporary block; paste this first.   |
| `proxies.yaml`          | ProxyClass + ingress ProxyGroup (2 replicas, HA).       |

## Install / cutover runbook

1. **Console, one sitting** (admin console at login.tailscale.com):
   - Access controls: paste `policy.cutover.hujson`.
   - Machines: change witness1's tag from `tag:itsbagelbot` to `tag:witness`.
   - DNS: ensure MagicDNS is on and **HTTPS Certificates** is enabled.
   - Settings -> Trust credentials: new **OAuth client**, write scope on
     `Devices: Core`, `Keys: Auth Keys`, and `Services`, each with tag
     `tag:k8s-operator`. Keep the client id and secret for step 2.
2. **Operator install** (id/secret in env, never in git):
   ```sh
   helm repo add tailscale https://pkgs.tailscale.com/helmcharts
   helm repo update
   helm upgrade --install tailscale-operator tailscale/tailscale-operator \
     --namespace=tailscale --create-namespace \
     --set-string oauth.clientId="$TS_OAUTH_CLIENT_ID" \
     --set-string oauth.clientSecret="$TS_OAUTH_CLIENT_SECRET" \
     --set-string operatorConfig.hostname="k8s-operator" \
     --set-string apiServerProxyConfig.mode="true" \
     --wait
   kubectl apply -f deploy/tailscale/proxies.yaml
   ```
3. **Admin over the operator**: `kubectl apply -f deploy/k8s/admin.yaml`
   (the tailscale-class Ingress), wait for `https://admin.tail451e6d.ts.net`,
   then delete the old traefik IngressRoute if present.
4. **kubectl over the API server proxy**:
   `tailscale configure kubeconfig k8s-operator` on each operator device,
   verify `kubectl get nodes` works through it.
5. **Traefik off the node interfaces**:
   `kubectl apply -f deploy/k8s/traefik-config.yaml` (2 replicas,
   ClusterIP; svclb goes away). Verify the public dashboard still serves.
6. **Lockdown**: paste `policy.hujson` (drops the temporary block; the
   nodes now accept only SSH from `tag:macbook`). Verify SSH, kubectl,
   the admin UI, the public dashboard, and Valkey sentinel quorum
   (`SENTINEL master`s from a sentinel container).

Break-glass with the operator down: SSH to node1, `sudo k3s kubectl`.
