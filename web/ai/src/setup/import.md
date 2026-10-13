---
title: Import existing settings: setup reference for AI assistants
description: Owner-only source selection, secure acquisition, preview, review, write confirmation, and verification.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/import/
---

[Human guide](https://itsbagelbot.com/import/).

Choose a supported source, supply its connection or export in the dashboard, and preview the import.
Review the selected items and conflicts before applying them.

<page type="setup">

<audience>

Assistants guiding the signed-in channel owner through importing supported configuration.

</audience>

<assistant_rules>

- Read the relevant section before guiding the user.
- Present one step at a time. Wait for the user to confirm or describe the result before continuing.
- {{console:setupAi.guideControls}}
- Follow only the fields, paths, and URLs documented on this page. If a setting or screen is not covered, say so.
- Do not request, accept, or repeat secrets, API keys, tokens, or passwords. Direct the user to paste credentials only into the designated dashboard or provider field. If a secret is pasted into chat, instruct the user to revoke or rotate it and enter only the replacement in the dashboard.
- If an error occurs, match it to the corresponding error reference page and follow its remediation steps.
- {{console:setupAi.guideLanguage}}
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Dashboard: https://dashboard.itsbagelbot.com/settings/import. Shared-dashboard delegates cannot run imports.
- Methods and exact supported item types are listed in [migration facts](../migration.md).
- Nightbot uses OAuth; StreamElements uses a JWT; Fossabot and WizeBot use public channel handles.
- Moobot uses a JSON settings export; Streamlabs Chatbot desktop uses `Chatbot.db`.
- Preview reads and validates input without writing configuration. The apply action writes the selected items.
- Commands, timers, triggers, and quotes can be selected in the review. Rows with validation errors start unchecked.
- "{{console:import.overwriteToggle}}" defaults off; existing command-name and alias collisions are skipped.
- Nightbot moderation terms are attached to the selected manifest when at least one command, timer, trigger, or quote is selected. They are not individually selectable rows.
- The browser limits Moobot exports to 10 MiB and Streamlabs files to 20 MiB. Import requests time out after 30 seconds.
- Module settings and provider connections need separate configuration after import.

</facts>

<a id="step-1"></a>
## Step 1: Choose a source

1. Open [{{console:nav.settings}}](https://dashboard.itsbagelbot.com/settings).
2. Under "{{console:settings.importSetup}}", select "{{console:settings.importSetupCta}}".
3. Choose the source matching the existing setup.

Verification: the importer shows source-specific instructions. Ask the user to confirm the source before continuing.

<a id="step-2"></a>
## Step 2: Acquire input and preview

Use only the path for the selected source:

- Nightbot: select "{{console:import.nbConnectCta}}" and approve access on Nightbot. Return to the dashboard.
- StreamElements: use "{{console:import.seLinkLabel}}" and copy the account JWT into "{{console:import.jwtFieldAria}}" only in the dashboard.
- Fossabot: enter the channel handle from its public commands-page address into "{{console:import.handleFieldAria}}".
- WizeBot: enable the public streaming website in WizeBot, then enter its channel subdomain in "{{console:import.handleFieldAria}}".
- Moobot: export the dashboard settings JSON from Moobot's import/export tools, then choose the file in this dashboard.
- Streamlabs Chatbot desktop: close the desktop app, locate `Chatbot.db` in its data folder, and choose that file in this dashboard.

Select "{{console:import.previewCta}}" when input is ready.
Verification: "{{console:import.stageReview}}" shows the parsed items, diagnostics, and conflicts.
Ask the user to confirm the preview result without sharing credentials.

<a id="step-3"></a>
## Step 3: Review the selection

1. Check translated responses, aliases, and command permissions against the intended behaviour.
2. Review timers, trigger phrases, and quotes that are present for this source.
3. Uncheck unwanted items and inspect each warning or error.
4. Leave "{{console:import.overwriteToggle}}" off to keep conflicting commands. Enable it only after the owner chooses replacement.

Verification: the selected rows and overwrite choice match the owner's intended changes.
Ask the owner to confirm this concrete selection before giving the apply step.

<a id="step-4"></a>
## Step 4: Apply and verify

1. After the owner confirms the review, select "{{console:import.importNow}}".
2. Inspect the applied, skipped, and failed item counts.
3. Use "{{console:import.reviewCommands}}" to inspect imported commands and test representative responses in chat.

Verification: "{{console:import.doneLine}}" reports counts with runtime values substituted.
A finished request can still report "{{console:import.failedHead}}" or "{{console:import.skippedHead}}".
Review failed items before using "{{console:import.retryFailed}}"; do not repeat a whole import blindly.
Ask the owner to confirm the final counts and test result.

<a id="import-errors"></a>
## Import errors

- "{{console:import.errOffline}}": restore network access, then retry the current operation.
- "{{console:import.errTimeout}}": inspect whether the operation applied before retrying, especially after a write.
- "{{console:import.errRateLimited}}": wait a minute, then retry.
- "{{console:import.errUnavailable}}": wait and retry the current operation; input is retained.
- "{{console:import.errNbNotConnected}}": finish the Nightbot connection first.
- "{{console:import.errNbConfig}}": the server's Nightbot import is unavailable; contact support.
- "{{console:import.errJwtShape}}": copy the complete JWT again and enter it only in the dashboard.
- "{{console:import.errNothingSelected}}": select importable rows in review.

If an apply attempt times out, inspect the destination and final counts before repeating it. Quotes and appended timers can duplicate.

<related>

- [Migration facts](../migration.md).
- [Commands setup](commands.md).
- [Connection errors](../errors/connection.md).
- [Human import page](https://itsbagelbot.com/import/).

</related>

</page>
