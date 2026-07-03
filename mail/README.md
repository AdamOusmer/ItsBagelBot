# Support email templates

Paste-ready email templates for the support team. Same look as the
transactional emails the bot sends (the Premium gift email), so a hand-written
support reply and an automated receipt feel like they came from the same place.

These are **static files for humans**, not code. Nothing here is wired into a
service. You copy the file, fill it in, and paste it into whatever tool sends
the mail (Resend dashboard, Gmail with an HTML-paste add-on, a help desk).

## Files

| File | Use |
| --- | --- |
| `support-email.html` | The template. Copy it, replace every `[[ ... ]]`, paste as the **HTML** body. |
| `support-email.txt`  | Plain-text twin. Paste as the **plain-text** body so text-only clients get a clean version. |
| `example-filled.html` | The template with everything filled in. Open in a browser to see the finished look. Do **not** send this one. |
| `received.html` / `received.txt` | Ready-to-send auto-acknowledgment: confirms the message landed, states open hours (10am–5pm most active days), and links the Discord for faster replies. Fire it on receipt. Only edit the hours line if the window changes. |

## Recognising us / anti-phishing

Support mail is a prime phishing target, so every template carries the same two
fixed pieces. **Do not edit, personalise, or remove them.** Their whole value is
that they are identical on every email, so users learn the pattern.

1. **Verified-sender strip** (header, under the wordmark): a green `✓ Official
   ItsBagelBot email · itsbagelbot.com`. This is the at-a-glance "it's really
   us" cue. Same tick, same wording, same place, always.
2. **Anti-phishing footer**: "ItsBagelBot will never ask for your password,
   payment or card details, or login and verification codes by email. We only
   send from @itsbagelbot.com..." with a Discord link to verify anything
   suspicious.

Together with the locked visual identity (logo + wordmark, the tan→green accent
bar, the bagel-stamp signature) these give the mail a look that is easy to
recognise and awkward to fake convincingly.

### The strip is not real authentication, the domain is

A tick in the body proves nothing on its own, anyone can copy pixels. What
actually stops spoofing is the sending domain, set up on the DNS / Resend side.
Full state and runbook: [`deploy/dns/email-auth.md`](../deploy/dns/email-auth.md).

- **SPF, DKIM, DMARC** on `itsbagelbot.com` — live. DKIM is set for both Zoho
  and Resend, both aligned. DMARC is at `p=quarantine; pct=100` (spoofed
  `@itsbagelbot.com` goes to spam); ramp to `p=reject` after ~2 weeks of clean
  reports.
- **BIMI** — record live at `default._bimi.itsbagelbot.com`, logo at
  [`web/public/bimi.svg`](../web/public/bimi.svg). Goes live on the next web
  deploy; Gmail/Apple then show the logo **once a VMC is added** (paid cert,
  needs a trademark — see the runbook).
- **One consistent From identity**, e.g. `ItsBagelBot Support
  <support@itsbagelbot.com>`, and a matching subject prefix such as
  `[ItsBagelBot]`. Consistency is what trains recognition.

Still owed by you: create the `dmarc@itsbagelbot.com` mailbox so reports land,
then ramp DMARC to reject and buy the VMC. All in the runbook.

## Sending a reply

1. Open `support-email.html` and `support-email.txt`.
2. Replace every `[[ ... ]]` marker with your copy. The marker text says what
   goes there.
3. Delete the blocks you do not need. The **note callout**, the **status line**,
   and the **button** each have a `OPTIONAL` banner comment marking where they
   start and end. Remove the whole block, banners included.
4. In your sender, set the HTML body from `support-email.html` and the
   plain-text body from `support-email.txt`. Always send both.
5. Send yourself a test first. Check it on a phone too.

## Design (do not "tidy up")

Email clients are not browsers. The rules that keep this rendering right:

- **Table layout + inline styles only.** No `<div>` grids, no external or
  `<style>`-block CSS (the one `@import` for the webfont is the exception, and
  clients that drop it just fall back to Arial/Helvetica).
- **One remote image, the logo.** Many clients block images by default, so the
  design has to still read with images off. Do not add more.
- **Colours and fonts are locked** to the site's system. They match
  `web/src/styles/style.css` and `app/transactions/mail/gift_template.go`:

  | Token | Value |
  | --- | --- |
  | Canvas | `#0a0a0a` |
  | Card | `#111110` |
  | Border | `#2b241b` |
  | Tan (accent) | `#c9a87c` |
  | Tan light | `#e0c49a` |
  | Text | `#f0ece4` / body `#a89f92` |
  | Muted | `#888077` |
  | Green (done) | `#52b788` |
  | Display font | Syne |
  | Body font | DM Sans |
  | Label / mono | DM Mono |
  | Signature | Caveat |

If the styling ever changes on the site or in the gift email, update these
files to match so support mail does not drift.
