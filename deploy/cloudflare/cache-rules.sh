#!/usr/bin/env bash
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
#
# Applies the zone-level http_request_cache_settings ruleset (Cache Rules) for
# the console hostnames. Run after deploying any dashboard change that touches
# edgeCacheControl in console/dashboard/src/hooks.server.ts.
#
# Design: ONE broad eligibility rule for all four console hosts with Edge and
# Browser TTL both "respect origin". Cloudflare never caches HTML by default
# and its cache key ignores Vary, so caching policy lives entirely in origin
# Cache-Control headers: edgeCacheControl marks anonymous default-render pages
# cacheable, everything else sends no-store/private and bypasses on its own.
# Broad eligibility is therefore safe by construction -- a path becomes
# cacheable exactly when the app says so, and a future public page needs no
# rule change here. There is deliberately no explicit /_app/immutable override:
# sirv's immutable header + respect_origin gives the year-long edge TTL without
# ever pinning a stale or error body for a year if something upstream strips
# headers. Default extension-based caching already covers those assets either
# way (verified live: cf-cache-status MISS -> HIT before this ruleset existed).
#
# The bagel_session bypass is defense-in-depth: even if the origin gate in
# edgeCacheControl ever regressed, a request carrying a session cookie can
# neither populate nor hit the edge cache. Cache rules are first-match-wins,
# so it must stay above the eligibility rule.
#
# This script OWNS the phase: the PUT replaces the whole ruleset. Foreign rules
# added through the dashboard will not survive a re-run of this script -- treat
# this file as the source of truth for the phase.
#
# Needs: CF_API_TOKEN (Zone > Cache Rules > Edit) and CF_ZONE_ID. Idempotent.
set -euo pipefail

: "${CF_API_TOKEN:?Set CF_API_TOKEN (Zone.Cache Rules:Edit)}"
: "${CF_ZONE_ID:?Set CF_ZONE_ID}"

API="https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/rulesets/phases/http_request_cache_settings/entrypoint"

PAYLOAD=$(cat <<'JSON'
{
  "rules": [
    {
      "description": "[bagelbot] bypass edge cache for session-bearing requests",
      "expression": "(http.cookie contains \"bagel_session=\")",
      "action": "set_cache_settings",
      "action_parameters": { "cache": false },
      "enabled": true
    },
    {
      "description": "[bagelbot] console hosts: eligibility on, TTLs from origin headers",
      "expression": "(http.host in {\"dashboard.itsbagelbot.com\" \"stats.itsbagelbot.com\" \"commands.itsbagelbot.com\" \"leaderboard.itsbagelbot.com\"})",
      "action": "set_cache_settings",
      "action_parameters": {
        "cache": true,
        "edge_ttl": { "mode": "respect_origin" },
        "browser_ttl": { "mode": "respect_origin" }
      },
      "enabled": true
    }
  ]
}
JSON
)

auth=(-H "Authorization: Bearer ${CF_API_TOKEN}" -H 'Content-Type: application/json')

echo '--- current phase entrypoint (empty phase is fine):'
curl -sS "${auth[@]}" "$API" | python3 -c '
import json, sys
d = json.load(sys.stdin)
if not d.get("success"):
    print(json.dumps(d.get("errors"), indent=2))
elif d.get("result"):
    print(json.dumps(d["result"].get("rules", []), indent=2))
else:
    print("(phase has no entrypoint yet)")'

echo '--- applying:'
RESULT=$(curl -sS "${auth[@]}" -X PUT "$API" -d "$PAYLOAD")

python3 - "$RESULT" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
if not d.get("success"):
    print(json.dumps(d.get("errors"), indent=2))
    sys.exit(1)
print(f"applied {len(d['result'].get('rules', []))} rule(s), version {d['result'].get('version')}")
PY

echo '--- verify a public page picks up HIT within its s-maxage:'
echo "    curl -sI https://stats.itsbagelbot.com/ | grep -iE 'cf-cache-status|cache-control'"
