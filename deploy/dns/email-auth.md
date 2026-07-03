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

## Open items to reach "fully enforced + branded"

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

### 3. BIMI logo must be live  — ships with the next web deploy

`l=` points at `https://itsbagelbot.com/bimi.svg`. The file is
[`web/public/bimi.svg`](../../web/public/bimi.svg) (SVG Tiny PS: `baseProfile=
tiny-ps`, one `<title>`, square viewBox, no CSS/script/external refs). It goes
live when the web image builds and Flux rolls it. Until then the URL 404s and
clients fall back to the normal avatar (no harm). Verify after deploy:

```sh
curl -sI https://itsbagelbot.com/bimi.svg | grep -i content-type   # want image/svg+xml
```

### 4. VMC (Verified Mark Certificate)  — required for Gmail / Apple / Yahoo

The bare `l=` record is valid and some clients (Fastmail, La Poste) render it
once the SVG is live. **Gmail, Apple Mail and Yahoo only show the logo when the
record also carries `a=` pointing at a VMC.** A VMC is a paid certificate
(~$1k+/yr, DigiCert or Entrust) issued against a **registered trademark** of the
logo (a Common Mark Certificate / CMC is the trademark-free alternative Apple
accepts, still paid). Steps:

1. Register the ItsBagelBot logo as a trademark (or gather CMC prior-use
   evidence). This is the long pole, months.
2. Buy the VMC from DigiCert/Entrust using the exact SVG in `web/public/bimi.svg`.
3. Host the issued PEM at `https://itsbagelbot.com/vmc.pem` (drop it in
   `web/public/vmc.pem`, deploy).
4. Add the `a=` tag: run [`bimi-add-vmc.sh`](bimi-add-vmc.sh).
