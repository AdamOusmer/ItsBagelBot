<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Cilium (manual Helm release)

Cilium is installed with `helm upgrade --install`, not by Flux -- it is the
CNI underneath the cluster Flux itself runs on, so it cannot depend on the
thing it bootstraps. Nothing in `deploy/flux/` or `deploy/infra/` reconciles
anything here, and nothing reverts a manual change made against the live
release either. This directory exists so that any resource applied
out-of-band still has a record in the repo of what should be live and why,
even though applying it is a manual step.

## Resources

- `cp1-legacy-masquerade-nodeconfig.yaml` -- `CiliumNodeConfig` that disables
  BPF masquerade on cp1 only, because cp1's egress interface has a secondary
  public /32 the underlying cloud VNIC never registered, and BPF masquerade
  SNATs to that address regardless of route selection. See the comment at the
  top of the file for the full failure mode. Apply with:

  ```
  kubectl apply -f deploy/cilium/cp1-legacy-masquerade-nodeconfig.yaml
  ```

  Re-apply after any Cilium reinstall/upgrade that touches cp1 -- a fresh
  install will not recreate this on its own.

## Checking what's actually live

`CiliumNodeConfig` and Helm values drift silently since nothing here is
enforced. Before trusting this directory, diff it against the cluster:

```
kubectl get ciliumnodeconfig -n kube-system -o yaml
helm get values cilium -n kube-system
```
