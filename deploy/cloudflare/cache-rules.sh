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
# neither populate nor hit the edge cache.
#
# That bypass is expressed TWICE -- as a "not" clause on the eligibility rule
# and as a standalone rule after it -- because this phase does not terminate on
# match. Custom Rules stop at the first match; Cache Rules do not: every
# matching rule applies, and a later rule overrides an earlier one for the same
# setting. The first version of this file assumed first-match-wins and put the
# standalone bypass rule ABOVE eligibility. Measured on the live zone
# 2026-09-01, that shape did nothing: a request carrying
# `Cookie: bagel_session=...` to leaderboard.itsbagelbot.com/itsmavey returned
# cf-cache-status HIT four times running, because the eligibility rule's
# cache:true silently overwrote the bypass rule's cache:false. With the
# not-clause added and the standalone rule moved below, the same request
# measures DYNAMIC while the anonymous request still measures MISS -> HIT.
# Do not collapse this back to a single rule or restore the old order: the
# not-clause is what actually holds today, and the standalone rule is what
# holds if the clause is ever dropped.
#
# bagel_cursor=0 is excluded for a different reason: correctness, not defence.
# Cloudflare's cache key ignores cookies, so a cookie that changes the rendered
# markup has to be named here or the edge will serve one visitor's variant to
# another. The cursor opt-out flips markup server-side (hooks.server.ts returns
# null from edgeCacheControl for it), but "origin says no-store" only helps on
# requests that reach the origin -- and a cached page means none do. Measured
# 2026-09-01 before this clause existed: `Cookie: bagel_cursor=0` on
# leaderboard.itsbagelbot.com/itsmavey returned HIT, serving the cursor-ON body
# to a visitor who opted out. Only '0' is matched because the cookie is only
# ever written '0' or '1' (session.ts) and absent means on, so cursor=1 and no
# cookie share one cacheable variant. Any FUTURE cookie that changes markup
# must be added here too -- that is the one case where the "broad eligibility
# is safe by construction" claim above does not hold on its own.
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
      "description": "[bagelbot] console hosts: eligibility on, TTLs from origin headers",
      "expression": "(http.host in {\"dashboard.itsbagelbot.com\" \"stats.itsbagelbot.com\" \"commands.itsbagelbot.com\" \"leaderboard.itsbagelbot.com\"} and not http.cookie contains \"bagel_session=\" and not http.cookie contains \"bagel_cursor=0\")",
      "action": "set_cache_settings",
      "action_parameters": {
        "cache": true,
        "edge_ttl": { "mode": "respect_origin" },
        "browser_ttl": { "mode": "respect_origin" }
      },
      "enabled": true
    },
    {
      "description": "[bagelbot] bypass edge cache for session or cursor-opt-out requests",
      "expression": "(http.cookie contains \"bagel_session=\" or http.cookie contains \"bagel_cursor=0\")",
      "action": "set_cache_settings",
      "action_parameters": { "cache": false },
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
