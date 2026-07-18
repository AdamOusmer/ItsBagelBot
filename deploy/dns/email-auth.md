# Email authentication (SPF / DKIM / DMARC / BIMI)

Records for `itsbagelbot.com`, Cloudflare zone `21c8bf01d6c33a5a1bc78eda7b23f84d`.
Two senders: **Zoho** (human support mail, `@itsbagelbot.com`) and **Resend**
(transactional, via the `send.` subdomain). This file is the source of truth for
what the zone should contain, the zone itself is edited through the Cloudflare
API using the `CLOUDFLARE_API_TOKEN` in Doppler project `cloudflared`, config
`prd`. There is no DNS-as-code apply step, the records are managed by hand.

## Current state (all live)

| Record | Name | Value | Owner |
| --- | --- | --- | --- |
| MX | `itsbagelbot.com` | `mx.zohocloud.ca` (10), `mx2` (20), `mx3` (50) | Zoho |
| SPF | `itsbagelbot.com` TXT | `v=spf1 include:zohocloud.ca ~all` | Zoho |
| DKIM | `zmail._domainkey` TXT | `v=DKIM1; k=rsa; p=…` | Zoho |
| MX | `send.itsbagelbot.com` | `feedback-smtp.us-east-1.amazonses.com` (10) | Resend |
| SPF | `send.itsbagelbot.com` TXT | `v=spf1 include:amazonses.com ~all` | Resend |
| DKIM | `resend._domainkey` TXT | `p=…` | Resend |
| DMARC | `_dmarc.itsbagelbot.com` TXT | `v=DMARC1; p=quarantine; sp=quarantine; adkim=r; aspf=r; pct=100; rua=mailto:dmarc@itsbagelbot.com; ruf=mailto:dmarc@itsbagelbot.com; fo=1` | shared |
| BIMI | `default._bimi.itsbagelbot.com` TXT | `v=BIMI1; l=https://itsbagelbot.com/bimi.svg;` | shared |

### Alignment (why quarantine is safe)

DMARC passes if SPF **or** DKIM aligns. Both senders DKIM-sign with
`d=itsbagelbot.com`, so both align regardless of the envelope path:

- **Zoho**: From `@itsbagelbot.com`; SPF `include:zohocloud.ca` aligns, DKIM
  `zmail` (d=root) aligns. Passes.
- **Resend**: envelope MAIL FROM is `send.itsbagelbot.com` (relaxed SPF aligns),
  and DKIM `resend` (d=root) aligns strictly. Passes on DKIM even if SPF is
  treated strict.

`sp=quarantine` covers spoofed subdomains. It does not affect Resend: Resend's
visible From is the root domain, governed by `p=`, not `sp=`.

## Open items

### 1. `dmarc@itsbagelbot.com` mailbox  — REQUIRED

Aggregate (`rua`) and forensic (`ruf`) reports are sent here. Create the mailbox
or an alias/catch-all in Zoho, otherwise every report bounces and you get zero
visibility. Same-domain `rua`, so no external authorization record is needed.

### 2. Ramp DMARC to `p=reject`  — after ~2 weeks of clean reports

Once reports confirm no legitimate source is failing, raise the policy. One
call (looks the record up by name, no hardcoded id):

```sh
ZONE=21c8bf01d6c33a5a1bc78eda7b23f84d
doppler run -p cloudflared -c prd -- sh -c '
  id=$(curl -s -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
    "https://api.cloudflare.com/client/v4/zones/'"$ZONE"'/dns_records?type=TXT&name=_dmarc.itsbagelbot.com" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)[\"result\"][0][\"id\"])")
  curl -s -X PATCH -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H "Content-Type: application/json" \
    "https://api.cloudflare.com/client/v4/zones/'"$ZONE"'/dns_records/$id" \
    --data "{\"content\":\"v=DMARC1; p=reject; sp=reject; adkim=r; aspf=r; pct=100; rua=mailto:dmarc@itsbagelbot.com; ruf=mailto:dmarc@itsbagelbot.com; fo=1\"}"
'
```

## BIMI: free tier, no certificate (decided)

We run BIMI **without** a VMC or CMC. The record is `l=` only (logo URL, no
`a=`). This is a deliberate choice, not an unfinished step:

- The logo file (`web/public/bimi.svg`, served by Cloudflare Pages at
  `https://itsbagelbot.com/bimi.svg`, SVG Tiny PS) is currently **removed**:
  the old file carried a placeholder mark that was not our logo, and the real
  logo only exists as raster PNG, which BIMI does not allow. The record still
  points at the URL; clients treat the 404 as "no BIMI" and fall back to a
  monogram avatar. Mail delivery and DMARC are unaffected. Re-add the file
  once a vector version of the real logo exists — clients that do not require
  a certificate (Fastmail, La Poste, and others) will then show it.
- **Gmail, Apple Mail and Yahoo will not show the logo** without an `a=`
  certificate. We accept that. A VMC (~$1k+/yr, needs a *registered* trademark)
  or CMC (similar price, Apple-only, no Gmail) is not worth it: the logo is
  cosmetic, and the actual anti-spoofing protection is DMARC, which is free and
  already enforced.

If that calculus ever changes: buy a VMC/CMC against the exact SVG, host the
PEM at `https://itsbagelbot.com/vmc.pem`, then add `a=https://itsbagelbot.com/vmc.pem`
to the BIMI record (same CF API PATCH pattern as the DMARC ramp above).

Verify the logo is served correctly:

```sh
curl -sI https://itsbagelbot.com/bimi.svg | grep -i content-type   # want image/svg+xml
```
