# Human email template context

Use [shared terminology](../CONTEXT.md) and the [context map](../CONTEXT-MAP.md).
This directory holds English/French support mail artifacts; it is not an executable app/package.

## Responsibility and boundaries

- Gives support operators consistent HTML and plain-text copy templates in English and French.
- A human fills a copy and sends it with an external mail tool; no service imports these files.
- There is no build, server, package manifest, route, database or automated delivery hook here.
- Automated Premium/gift/giveaway mail belongs to [transactions mail](../app/db/transactions/mail/mail.go).
- The transaction service owns automated recipient/delivery state and Resend integration.
- Editing these files changes future manually copied content, not service-sent email.
- Site/legal copy belongs to [marketing](../web/marketing/context.md); coordinate claims with those sources.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Support reply | Human-written answer using the reusable support template. |
| Received acknowledgment | Copy confirming receipt and describing response expectations. |
| Preheader | Hidden first line used as inbox preview text. |
| Marker / placeholder | `[[ ... ]]` text that must be replaced before sending. |
| CTA | Optional call-to-action link/button with its own URL and label. |
| Plain-text twin | Alternative body preserving the HTML message's actual information. |
| Verified-sender strip | Fixed branded recognition copy; not cryptographic sender authentication. |
| Anti-phishing footer | Fixed notice about information ItsBagelBot will never request by email. |

Premium gifts/awards and Tebex subscriptions remain distinct under the root glossary.
An acknowledgment means the message arrived; it does not claim resolution or immediate response.

## File navigation

- [support-email.html](support-email.html): master support reply with markers and optional sections.
- [support-email.txt](support-email.txt): plain-text alternative for the same message.
- [example-filled.html](example-filled.html): visual example; not a real reply to send.
- [received.html](received.html) and [received.txt](received.txt): ready acknowledgment body alternatives.
- [support-email.fr.html](support-email.fr.html) and [support-email.fr.txt](support-email.fr.txt): French reply pair.
- [received.fr.html](received.fr.html) and [received.fr.txt](received.fr.txt): French acknowledgment pair.
- [example-filled.fr.html](example-filled.fr.html): French visual demonstration, never a real reply to send.
- [README.md](README.md): operator editing/sending procedure and email-client styling rationale.
- [gift_template.go](../app/db/transactions/mail/gift_template.go): automated gift-specific copy/escaping.
- [premium_template.go](../app/db/transactions/mail/premium_template.go): shared transactional layout.
- [giveaway_template.go](../app/db/transactions/mail/giveaway_template.go): automated award content.

## Editing and delivery flow

- Copy the intended template before personalizing; avoid turning the master into one recipient's message.
- Replace every marker, including hidden preheader content and CTA URL/label.
- Support HTML marks optional note, status and button blocks with `OPTIONAL` comments.
- Remove whole unwanted blocks and keep the plain-text body semantically synchronized.
- Choose matching language/body alternatives; French markers intentionally use the same handoff format.
- Sender configuration selects subject, From identity, recipient and actual delivery mechanism.
- This directory does not configure sender-domain DNS, mailboxes or automatic acknowledgment triggers.

## Design and content invariants

- Preserve fixed verified-sender strip and anti-phishing copy within each language; README prohibits
  personalization and requires faithful meaning when adding another language.
- Use email-compatible tables/inline styles; avoid converting layout into browser-only grids/components.
- Fonts have fallback stacks because clients can discard imported webfonts.
- The remote logo can be blocked; the message must remain understandable with images disabled.
- Preserve email-client rendering techniques after visual comparison; inspect the actual target
  rather than assuming browser CSS support.
- Do not copy Svelte/Astro components into mail HTML; email rendering has different constraints.
- Link to real intended destinations and escape inserted HTML-sensitive characters where appropriate.
- Keep promised hours/contact instructions identical in HTML/text acknowledgment alternatives.

## Verification and known limits

From repository root: `rg -n '\[\[|OPTIONAL' mail` finds master markers/optional blocks.
For a prepared copy, search that copy for remaining `[[` and review all visible/hidden copy and links.
Open the prepared HTML and compare with its plain-text version; then send a test to yourself only
when sending has been authorized, checking desktop/mobile and image-blocked behavior.
There is no mail-directory test command. Automated template tests live with transactions code;
they do not validate these static human templates.
README refers to old `web/src/styles/style.css`; current marketing styles live under `web/marketing/`.
Its SPF/DKIM/DMARC/BIMI operational assertions are historical notes, not verified live status;
check the actual configuration when asked to change delivery/authentication behavior.
