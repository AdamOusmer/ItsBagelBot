#!/bin/bash
echo "Monitoring pipeline..."
MSG=$(git log -1 --pretty=%s)
while true; do
  pending=$(gh run list --limit 10 | grep "$MSG" | grep -E "in_progress|queued")
  if [ -z "$pending" ]; then
     break
  fi
  sleep 10
done

status=$(gh run list --limit 10 | grep "$MSG" | head -n 1)
echo "Pipeline finished with status: $status"

echo "Reconciling flux (if applicable)..."
flux reconcile source git flux-system 2>/dev/null || true
flux reconcile kustomization flux-system --with-source 2>/dev/null || true

echo "Waiting for deployments to roll out..."
deps="commands console-admin console-dashboard modules outgress projector transactions twitch-ingress users worker"
for dep in $deps; do
  kubectl rollout status deployment/$dep -n production --timeout=3m || true
done

echo "Final pod status:"
kubectl get pods -n production
