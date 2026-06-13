# Flux GitOps Runbook

## Overview

GitHub Actions builds multi-arch images (ARM + Intel, natively on separate runners) and
pushes them to GHCR tagged `main-<ts>-<sha>`, `sha-<sha>`, `main`, and `latest`, plus an
immutable digest reference. Flux runs in-cluster and is the only thing that mutates
deployments:

1. The Flux `image-reflector-controller` scans GHCR every 5 minutes via `ImageRepository`
   objects.
2. `ImagePolicy` objects select the newest image by extracting the numeric timestamp from
   the `main-<ts>-<sha>` tag (monotonically sortable).
3. `ImageUpdateAutomation` commits the updated image reference (tag + digest) back to
   `deploy/k8s/` on `main`.
4. The `apps` `Kustomization` reconciles `deploy/k8s/` and applies the rolling update.

No SSH. No manual `ctr import`. Each node pulls images directly from GHCR.

## Security

After Flux pins an image, manifests reference it as `tag@sha256:<digest>`, which is
immutable. The tag alone cannot be mutated without also changing the digest reference
already committed to git. CI also attests build provenance via
`actions/attest-build-provenance`.

GHCR packages are **public** (the repo is public), so neither the kubelet nor the
image-reflector-controller needs any registry credential: pulls and scans are anonymous.
This deliberately means there is **no GHCR token anywhere in the cluster**, so nothing can
reach the package registries of other organizations the account belongs to. If the packages
are ever made private, add a `ghcr-pull` dockerconfigjson secret in both the `production` and
`flux-system` namespaces (scoped to this personal account only, e.g. a fine-grained PAT) and
reference it from each `Deployment` (`imagePullSecrets`) and `ImageRepository` (`secretRef`).

## Anti-affinity and Rolling Update Notes

`admin` and `dashboard` use hard pod anti-affinity across the 2-node cluster. Rolling
updates on these services set `maxSurge: 0` / `maxUnavailable: 1` with a
`PodDisruptionBudget` of `minAvailable: 1`, ensuring one pod at a time rolls without
deadlocking. All other services use soft (preferred) anti-affinity and roll normally.

`nats` and `nats-leaf` are infrastructure components and are not managed by Flux image
automation.

## One-Time Bootstrap

1. Install the Flux CLI:
   ```
   brew install fluxcd/tap/flux
   ```

2. Export a GitHub token with `repo` scope for this personal repo only (the gh CLI token
   works: `export GITHUB_TOKEN=$(gh auth token)`). Bootstrap uses it once to create a
   repo-scoped **deploy key**; the persistent in-cluster credential is that deploy key (SSH,
   limited to this repo), never an account- or org-wide token. No `packages` scope is needed
   because the images are public.

3. Bootstrap Flux into the cluster (the `--components-extra` flag is REQUIRED to install
   the image automation controllers, which are not included by default):
   ```
   flux bootstrap github \
     --owner=AdamOusmer \
     --repository=ItsBagelBot \
     --branch=main \
     --path=deploy/flux/clusters/production \
     --personal \
     --components-extra=image-reflector-controller,image-automation-controller
   ```
   > Do NOT add a hand-written `kustomization.yaml` at the sync path
   > (`deploy/flux/clusters/production/`). Flux auto-generates one recursively that
   > includes the bootstrap `flux-system/` components plus our resources. A manual
   > kustomization.yaml that omits `flux-system` would make the self-managing
   > Kustomization (prune enabled) delete its own controllers.

   No pull-secret step is required while the packages are public.

## Verifying the Setup

```
flux get images all
flux get kustomizations
kubectl -n production get deploy -o wide
```

## End-to-End Deploy Flow

1. Merge a PR to `main`.
2. GitHub Actions builds and pushes `ghcr.io/adamousmer/itsbagelbot/<service>:main-<ts>-<sha>`.
3. Flux `ImageRepository` scans GHCR within 5 minutes and detects the new tag.
4. `ImagePolicy` selects the tag with the highest timestamp.
5. `ImageUpdateAutomation` commits the updated image reference to `deploy/k8s/` on `main`.
6. The `apps` `Kustomization` detects the change (10m interval or immediate via webhook)
   and applies the rolling update to the cluster.

## Legacy Scripts

The legacy SSH-based deploy scripts at the repo root (`build_and_deploy_fixed.sh`,
`build_all.sh`, etc.) are superseded by this GitOps workflow and should not be used.
