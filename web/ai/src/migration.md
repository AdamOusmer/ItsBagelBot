---
title: Import and migration facts: reference for AI assistants
description: Six verified import sources, acquisition methods, item types, review safeguards, and mapping limits.
updated: 2026-10-13
kind: migration
human: https://itsbagelbot.com/import/
---

[Human guide](https://itsbagelbot.com/import/).

ItsBagelBot imports selected configuration from six Twitch bot sources. Acquisition varies by source: OAuth, a JWT, a public channel lookup, or an uploaded file.
Preview and review happen before configuration is written.

<page type="migration">

<audience>

Assistants answering migration questions or helping the channel owner evaluate a configuration import.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Facts verified on 2026-10-13 against dashboard source strategies, parsers, and the import commit handler.
- Dashboard destination: [{{console:settings.importSetup}}](https://dashboard.itsbagelbot.com/settings/import) under "{{console:nav.settings}}".
- Imports are available to the signed-in channel owner, not a shared-dashboard delegate. No Premium gate is applied to the importer.
- "{{console:import.previewCta}}" parses and validates the source, returns diagnostics, and checks existing command names and aliases.
- "{{console:import.importNow}}" applies the reviewed selection. Items with validation errors cannot be applied.
- "{{console:import.overwriteToggle}}" is off by default. Conflicting commands are skipped unless replacement is selected.
- Supported command variables are translated. Unsupported variables may remain literal text with a diagnostic.
- Source roles with no equivalent can widen access. Review permission warnings before applying.
- Applied counts, skipped collisions, and failed items are reported separately. A successful request can still contain individual failures.
- Commands, timers, triggers, and quotes are distinct item types. The generic manifest also supports moderation term lists.
- Manifest validation caps: 2000 commands, 300 timers, 1000 triggers, and 5000 quotes.
- Imported timers and triggers are merged into existing module configuration and enable those modules. Review the result before going live.
- Imported quotes receive destination quote numbers; source numbering is not guaranteed to survive.
- General module settings are not part of supported exports. Connect Spotify, Govee, and Discord separately.
- Imported AutoMod terms do not remove the Premium beta gate on optional AutoMod functionality.

</facts>

## Sources, methods, and item types

| Source | Method in this importer | Items verified in the parser | Source-specific limits |
| --- | --- | --- | --- |
| Nightbot | "{{console:import.nbConnectCta}}" starts OAuth; access is used for this import | Custom commands, enabled timers, literal blacklist terms | Regex blacklist entries are skipped; a failed spam-filter read can leave terms unavailable |
| StreamElements | Paste the account JWT into "{{console:import.jwtFieldAria}}" in the dashboard | Custom commands, enabled timers, command keywords converted to triggers | Regex commands are skipped; preview shows variable and permission mapping warnings |
| Fossabot | Enter the public channel handle in "{{console:import.handleFieldAria}}" | Published custom chat commands and available aliases | Hidden commands and unpublished timers or keyword rules are absent from this public route |
| WizeBot | Enter the public streaming-site channel handle | Published chat-text commands and available aliases | Streaming website must be enabled; sound-only and screen-animation commands are skipped |
| Moobot | Upload the dashboard JSON settings export | Custom commands, aliases, enabled command timers | Timer references must resolve to exported commands; fixed alias arguments are dropped with a warning |
| Streamlabs Chatbot desktop | Upload `Chatbot.db`, a SQLite database from the desktop app | Commands, timers, and quotes | Script or external-file behaviour is not recreated; missing sections produce diagnostics |

### Files and credentials

Moobot JSON is parsed in the browser and submitted as a parsed manifest for preview.
The browser file limit is 10 MiB for Moobot and 20 MiB for Streamlabs Chatbot.
Close the desktop chatbot before copying `Chatbot.db` so the database is fully written.
Enter a StreamElements JWT only in the dashboard, never in assistant chat.
Nightbot OAuth credentials are handled by the connection flow; its connection cookie is cleared after commit.
Public handle imports need neither a source password nor a token.

### Review the result

Review translated response text, aliases, permission changes, timers, and diagnostics before writing.
WizeBot may publish a public description instead of the actual response; compare the review against the intended chat output.
Streamlabs timers fall back to 600 seconds when a per-timer interval cannot be read.
Moobot timers reference commands and can expand into separate destination timer messages.
Nightbot regular-only commands have no matching regular tier and can become open to everyone.
Preview is not a reservation of destination names. Check the final applied, skipped, and failed counts.
Keep migration advice neutral; whether to change bots depends on the user's requirements.

<related>

- [Import setup](setup/import.md).
- [Product](product.md).
- [Commands setup](setup/commands.md).
- [Connection errors](errors/connection.md).
- [Human import page](https://itsbagelbot.com/import/).

</related>

</page>
