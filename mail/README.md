<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
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
| `example-filled.fr.html` | French, fully filled demonstration of the reply template. Open in a browser to preview it. Do **not** send this one. |
| `received.html` / `received.txt` | Ready-to-send auto-acknowledgment: confirms the message landed, states open hours (10am–5pm most active days). Fire it on receipt. Only edit the hours line if the window changes. |
| `received.fr.html` / `received.fr.txt` | French version of the ready-to-send acknowledgment. Use the pair together. |
| `support-email.fr.html` / `support-email.fr.txt` | French versions of the reply template. The `[[ ... ]]` markers are intentionally identical to the English pair so the same handoff checklist applies. |

## Recognising us / anti-phishing

Support mail is a prime phishing target, so every template carries the same two
fixed pieces. **Do not edit, personalise, or remove them.** Their whole value is
that they are identical on every email in that language, so users learn the
pattern. The English and French templates each keep their own locked wording;
translate the meaning faithfully when adding another language, but do not vary
the security copy between messages in that language.

1. **Verified-sender strip** (header, under the wordmark): a green `✓ Official
   ItsBagelBot email · itsbagelbot.com`. This is the at-a-glance "it's really
   us" cue. Same tick, same wording, same place, always.
2. **Anti-phishing footer**: the Resend shell's wording for the file's
   language, the i18n strings `mail.safety.label` and `mail.safety.body`
   followed by " itsbagelbot.com.", with `itsbagelbot.com` linking to
   https://itsbagelbot.com. Copy it from the shell's catalog, never reword it.

Together with the locked visual identity (logo + wordmark, verified strip,
safety footer, Caveat signature, the green to gold line on top of the card (#62a593 to #d8bf91, as the ui's reading-progress bar); no bagel stamp, no emoji) these give the mail a look that is easy to
recognise and awkward to fake convincingly.

### The strip is not real authentication, the domain is

A tick in the body proves nothing on its own, anyone can copy pixels. What
actually stops spoofing is the sending domain (SPF, DKIM and DMARC), which is
configured on the DNS and mail-provider side, not in this directory.

Use one consistent From identity, e.g. `ItsBagelBot Support
<support@itsbagelbot.com>`, and a matching subject prefix such as
`[ItsBagelBot]`. Consistency is what trains recognition.

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
  `ui/styles/brand.css` and `app/db/transactions/mail/premium_template.go`:

  | Token | Value |
  | --- | --- |
  | Canvas | `#111113` |
  | Card (no border) | `#18181b`, radius 16 |
  | Inset / note box (no border) | `#121214`, radius 8 |
  | Rule | `#2e2e32` (footer rule `#232325`) |
  | Tan (eyebrow) | `#d8bf91`, 12px, tracking 0.18em |
  | Tan light (emphasis, links) | `#eadbbd` |
  | Text | `#f4f4f5` / body `#b4b4bc`, 16px |
  | Secondary text | `#c2c2c9` |
  | Muted | `#a1a1aa` |
  | Green (done) | `#62a593` |
  | Primary button | bg `#f4f4f5`, text `#111113`, radius 8, DM Sans 500 |
  | Heading | Syne 600, tracking -0.02em |
  | Wordmark | Syne 800, tracking -0.01em |
  | Display font | Syne |
  | Body font | DM Sans |
  | Label / mono | DM Mono |
  | Signature | Caveat, with Segoe Script / Bradley Hand / Snell Roundhand fallbacks for clients that skip web fonts |

  The `@import` loads Syne, DM Sans, DM Mono and Caveat. Translucent ui tokens
  are pre-blended to solid hex because email clients drop rgba and shadows.

If the styling ever changes on the site or in the transactional emails, update these
files to match so support mail does not drift.
