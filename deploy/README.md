# Deployment Layout

Everything the cluster runs is described here as code. Delivery is pull-based
[Flux CD](flux/README.md): CI publishes images to GHCR, Flux reconciles these
manifests and writes digest pins back to `main`. No SSH, no manual `kubectl apply`.

```
deploy/
├── k8s/        Application tier (production namespace): Deployments, Services,
│               network policies, and the NATS broker config. This is the
│               Flux "apps" Kustomization (path ./deploy/k8s); image automation
│               commits digest pins into these files.
│
├── infra/      Cluster infrastructure. One subdir per Flux Kustomization, kept
│               separate because each targets its own namespace / wait policy:
│   ├── cluster/    Cross-namespace infra: cloudflared, traefik, linkerd,
│   │               newrelic.            → "cluster" Kustomization  (wait: false)
│   ├── valkey/     Valkey sentinel cluster (statefulset + sentinel, valkey ns).
│   │                                    → "valkey" Kustomization   (wait: true)
│   └── tailscale/  Tailscale operator ProxyClass / ProxyGroup.
│                                        → "tailscale" Kustomization (wait: false)
│
├── flux/       Flux controllers, the cluster Kustomization CRs (apps / cluster /
│               valkey / tailscale), and image-automation policies. Bootstrap
│               sync path is deploy/flux/clusters/production. See flux/README.md.
│
└── ansible/    One-time bare-metal node provisioning (base tuning, firewall,
                k3s agent, SELinux, SSH removal, tailscale). Not Flux-managed;
                run via ansible/provision.sh. See ansible/README.md.
```

## Container build files

Each service owns its build file next to its code, not here:

- Go / Elixir services: `app/<service>/Containerfile`
- Console SSR apps: `console/{dashboard,admin}/Containerfile`

All are built from the repo root in `.github/workflows/publish-images.yml`, which
selects only the images affected by a diff and builds ARM + Intel natively.

## The infra Kustomizations stay separate on purpose

The `apps` Kustomization pins everything to `targetNamespace: production`. The
`cluster`, `valkey`, and `tailscale` Kustomizations leave `targetNamespace`
unset so each manifest's own namespace applies, and they differ on `wait`
(valkey reports StatefulSet health and blocks; the CRD-only ones do not). They
share the `deploy/infra/` parent for navigation but remain distinct reconcilers.
