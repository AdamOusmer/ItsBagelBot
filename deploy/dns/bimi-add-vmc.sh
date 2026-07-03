#!/usr/bin/env sh
# Add the a=<VMC> tag to the live BIMI record once a Verified Mark Certificate
# is hosted. Run AFTER web/public/vmc.pem is deployed and reachable. Looks the
# record up by name so there is no hardcoded record id. Idempotent: re-running
# just re-sets the same content.
#
#   sh deploy/dns/bimi-add-vmc.sh
#
# Token comes from Doppler (project cloudflared, config prd); never printed.
set -eu

ZONE=21c8bf01d6c33a5a1bc78eda7b23f84d
NAME=default._bimi.itsbagelbot.com
SVG=https://itsbagelbot.com/bimi.svg
VMC=https://itsbagelbot.com/vmc.pem

# Refuse to wire up a VMC that is not actually reachable, an unreachable a=
# breaks BIMI evaluation instead of just leaving it unbranded.
code=$(curl -s -o /dev/null -w '%{http_code}' -I "$VMC")
if [ "$code" != "200" ]; then
  echo "vmc.pem not reachable at $VMC (HTTP $code). Deploy it first." >&2
  exit 1
fi

doppler run -p cloudflared -c prd -- sh -c '
  set -eu
  id=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
    "https://api.cloudflare.com/client/v4/zones/'"$ZONE"'/dns_records?type=TXT&name='"$NAME"'" \
    | python3 -c "import sys,json;r=json.load(sys.stdin)[\"result\"];print(r[0][\"id\"]) if r else exit(\"BIMI record not found\")")
  curl -s -X PATCH -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H "Content-Type: application/json" \
    "https://api.cloudflare.com/client/v4/zones/'"$ZONE"'/dns_records/$id" \
    --data "{\"content\":\"v=BIMI1; l='"$SVG"'; a='"$VMC"';\"}" \
    | python3 -c "import sys,json;d=json.load(sys.stdin);print(\"ok:\",d[\"success\"],d.get(\"result\",{}).get(\"content\") or d.get(\"errors\"))"
'
