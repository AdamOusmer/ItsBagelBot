#!/usr/bin/env bash
set -euo pipefail

# Bootstrap helper: creates the ghcr-pull secrets in both the production and
# flux-system namespaces. Run once after flux bootstrap.
#
# Required environment variables:
#   GHCR_USER  - GitHub username (owner of the GHCR packages)
#   GHCR_PAT   - Personal access token or fine-grained token with read:packages scope

: "${GHCR_USER:?GHCR_USER must be set to your GitHub username}"
: "${GHCR_PAT:?GHCR_PAT must be set to a PAT with read:packages scope}"

echo "Creating ghcr-pull secret in namespace: production"
kubectl create secret docker-registry ghcr-pull \
  --namespace production \
  --docker-server=ghcr.io \
  --docker-username="${GHCR_USER}" \
  --docker-password="${GHCR_PAT}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Creating ghcr-pull secret in namespace: flux-system"
kubectl create secret docker-registry ghcr-pull \
  --namespace flux-system \
  --docker-server=ghcr.io \
  --docker-username="${GHCR_USER}" \
  --docker-password="${GHCR_PAT}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Done. ghcr-pull secrets created in production and flux-system namespaces."
