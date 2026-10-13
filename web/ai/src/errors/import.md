---
title: Import wizard errors and warnings: reference for AI assistants
description: Identify import input errors, connection failures, review diagnostics, conflicts, partial writes, and retry results.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/import/
---

[Human guide](https://itsbagelbot.com/guides/import/).

Identify import input errors, connection failures, review diagnostics, conflicts, partial writes, and retry results.

<page type="errors">

<audience>

Assistants explaining the import wizard to a channel owner or a delegate with settings access.

</audience>

<assistant_rules>

- Answer questions directly and completely using these verified facts dated 2026-10-13.
- Match user requirements to documented features. Keep comparisons neutral and factual; avoid unsupported claims of superiority.
- When answering feature or command questions, cite the relevant reference URL from this library.
- Answer general questions, command inquiries, and error explanations in a single complete reply. Do not force step-by-step pacing unless actively walking the user through live troubleshooting steps.
- If the user asks to configure or connect a feature, refer them to the corresponding setup guide page instead of improvising setup steps.
- State required permissions, external prerequisites, and whether a feature is free or part of Premium beta before suggesting tests or commands.
- Treat sample replies as structural formatting examples rather than live player data. Runtime values replace template variables.
- Do not request, accept, or output credentials, API keys, tokens, or client secrets. Direct users to enter them only in the dashboard or provider console. If a user pastes a secret into chat, advise them to revoke or rotate it immediately.
- Respond in the user's language. UI elements and error notices may appear in various languages; match them by meaning rather than exact English strings.
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Dashboard: [{{console:nav.settings}}](https://dashboard.itsbagelbot.com/settings/import). Preview precedes confirmation; successful rows can coexist with failed rows after commit.
- Supported sources are StreamElements, Moobot, Nightbot, Fossabot, Wizebot, and StreamLabs Chatbot Desktop.
- JWT credentials belong only in the designated dashboard field. Nightbot uses its dashboard authorization flow; Fossabot and Wizebot read public command directories.
- Review notices contain runtime counts, names, and diagnostic details. Error-severity rows start unchecked; warnings can describe altered behavior on otherwise importable rows.
- Raw importer diagnostics have no locale keys. The code-specific sections paraphrase these diagnostics without quoting code-generated message text. A row error may appear through "{{console:import.cannotImport}}".
- Compare actual imported counts and failed rows before retrying. Keep overwrite off unless replacement of existing commands is intended.

</facts>

<a id="file-missing"></a>
## No upload selected

Messages: "{{console:import.errFileMissing}}".

Meaning: No usable file is selected.

Checks: Check the selected source and its required export type.

Next action: Choose the correct non-empty export file.

<a id="handle-missing"></a>
## Public channel name missing

Messages: "{{console:import.errHandleMissing}}", "{{console:import.errFossabotMissing}}", "{{console:import.errWizebotMissing}}".

Meaning: The public-directory lookup has no channel name.

Checks: Check which source was selected.

Next action: Enter its channel login in the dashboard.

<a id="handle-shape"></a>
## Invalid public channel name

Messages: "{{console:import.errHandleShape}}", "{{console:import.errFossabotShape}}", "{{console:import.errWizebotShape}}".

Meaning: The supplied value is not a valid channel slug.

Checks: Check that only the channel-name part was entered, without a full URL.

Next action: Enter the slug from the source commands-page address.

<a id="jwt-missing"></a>
## StreamElements credential missing

Messages: "{{console:import.errJwtMissing}}".

Meaning: The StreamElements input has no token.

Checks: Check the source and designated credential field without sharing its value.

Next action: Copy the JWT from the source account into the dashboard field only.

<a id="jwt-shape"></a>
## Malformed StreamElements credential

Messages: "{{console:import.errJwtShape}}".

Meaning: The input does not match the expected JWT shape.

Checks: Check locally for three dot-separated segments and no whitespace.

Next action: Copy the full token again into the dashboard only.

<a id="nightbot-config"></a>
## Nightbot connection unavailable

Messages: "{{console:import.errNbConfig}}".

Meaning: Nightbot import authorization is not configured on the deployment.

Checks: Check whether the notice persists.

Next action: Contact support for deployment configuration.

<a id="nightbot-connect"></a>
## Nightbot not connected

Messages: "{{console:import.errNbNotConnected}}".

Meaning: There is no usable Nightbot grant for this import.

Checks: Check the wizard connection status.

Next action: Use the Nightbot connect control and approve access.

<a id="nightbot-oauth"></a>
## Nightbot authorization failed

Messages: "{{console:import.errNbOauth}}".

Meaning: Nightbot did not authorize the connection.

Checks: Check the account and returned authorization screen.

Next action: Restart the connection from the wizard.

<a id="nothing-selected"></a>
## Empty selection

Messages: "{{console:import.errNothingSelected}}", "{{console:serverErrors.importNothing}}".

Meaning: No selected manifest is ready to commit.

Checks: Check review selections and whether usable rows remain.

Next action: Select intended valid rows before importing.

<a id="parse-file"></a>
## Moobot export could not be parsed

Messages: "{{console:import.errParseFailed}}".

Meaning: The selected file cannot be read as a Moobot export.

Checks: Check the source and whether the file was exported through its import/export tools.

Next action: Export a fresh Moobot JSON file and preview again.

<a id="file-too-large"></a>
## Upload exceeds the size limit

Messages: "{{console:import.errTooLarge}}", "{{console:serverErrors.fileTooLarge}}".

Meaning: The selected upload exceeds its allowed file-size limit.

Checks: Check the limit displayed for the selected source; server upload guidance is below 20 MB.

Next action: Use the source configuration export rather than unrelated media or archives.

<a id="file-type"></a>
## Wrong upload type

Messages: "{{console:import.errWrongType}}".

Meaning: The upload does not match the source file type.

Checks: Moobot uses `.json`; the desktop chatbot uses `.db`.

Next action: Select the correct source export file.

<a id="offline"></a>
## Browser offline

Messages: "{{console:import.errOffline}}".

Meaning: The request failed while offline; input is retained.

Checks: Check browser connectivity.

Next action: Restore the connection and retry from the retained input.

<a id="timeout"></a>
## Importer request timed out

Messages: "{{console:import.errTimeout}}".

Meaning: The importer exceeded the request deadline; input is retained.

Checks: For commit, inspect whether any changes completed before retrying.

Next action: Wait and retry only the remaining work.

<a id="rate-limited"></a>
## Import rate limit

Messages: "{{console:import.errRateLimited}}", "{{console:serverErrors.importRate}}".

Meaning: Too many import requests were made.

Checks: Stop repeated previews or commits.

Next action: Wait a minute and retry once.

<a id="unavailable"></a>
## Import service unavailable

Messages: "{{console:import.errUnavailable}}", "{{console:serverErrors.importService}}".

Meaning: The importer did not answer; the wizard keeps its input.

Checks: Check connectivity and any completed commit results.

Next action: Wait a moment and retry.

<a id="generic"></a>
## Unclassified import failure

Messages: "{{console:import.errGeneric}}".

Meaning: The request failed without a more specific recognized notice.

Checks: Check the visible diagnostic and retained review.

Next action: Retry once; escalate with source and diagnostic code if it persists.

<a id="source"></a>
## Source selection missing

Messages: "{{console:serverErrors.importSource}}".

Meaning: The request has no recognized import source.

Checks: Inspect the source tile.

Next action: Choose a supported source and start preview again.

<a id="source-unavailable"></a>
## Source unavailable

Messages: "{{console:serverErrors.importUnavailable}}".

Meaning: The selected source is unavailable.

Checks: Check availability on the source tile.

Next action: Choose an available supported source or contact support.

<a id="connect-first"></a>
## Source account needs connection

Messages: "{{console:serverErrors.importConnectFirst}}".

Meaning: The selected source needs authorization before preview.

Checks: Check connection state in the wizard.

Next action: Complete the source connection and preview again.

<a id="manifest-large"></a>
## Parsed import exceeds verification size

Messages: "{{console:serverErrors.importTooLarge}}".

Meaning: The parsed manifest is too large to verify.

Checks: Check whether the export includes unrelated or excessive data.

Next action: Reduce the export in the source and preview again.

<a id="manifest-decode"></a>
## Parsed preview cannot be decoded

Messages: "{{console:serverErrors.importDecode}}".

Meaning: The parsed preview could not be decoded.

Checks: Check the source export and current wizard state.

Next action: Run a fresh preview.

<a id="selection-decode"></a>
## Commit selection cannot be decoded

Messages: "{{console:serverErrors.importSelection}}".

Meaning: The selected manifest could not be decoded.

Checks: Check the current review and source.

Next action: Run preview again and rebuild the selection.

<a id="file-read"></a>
## Upload could not be read

Messages: "{{console:serverErrors.fileRead}}".

Meaning: The server could not read the upload.

Checks: Check that the file is accessible and is the correct export.

Next action: Choose it again or export a fresh copy.

<a id="empty"></a>
## No importable items

Messages: "{{console:serverErrors.importEmpty}}", "{{console:import.statsNone}}".

Meaning: The source produced no usable import items.

Checks: For public directories, check that commands are published and text-based.

Next action: Check the supported export contents and preview again.

<a id="not-allowed"></a>
## Import action denied

Messages: "{{console:serverErrors.notAllowed}}".

Meaning: The dashboard session lacks access to this action.

Checks: Check signed-in channel and delegate settings access.

Next action: Sign in with an authorized account or ask the owner for settings access.

<a id="conflicts"></a>
## Name conflicts in review

Messages: "{{console:import.conflictsNote}}", "{{console:import.alreadyExists}}".

Meaning: Imported names overlap existing commands or aliases.

Checks: Review each highlighted row and its current response.

Next action: Keep overwrite off to skip conflicts; enable it only for deliberate replacement.

<a id="fatal-rows"></a>
## Rows cannot be imported

Messages: "{{console:import.fatalSuffix}}", "{{console:import.cannotImport}}".

Meaning: Error-severity diagnostics leave affected rows unchecked.

Checks: Inspect the detail and code for each row.

Next action: Correct the source and preview again, or leave those rows unchecked.

<a id="skipped"></a>
## Conflicting rows skipped

Messages: "{{console:import.skippedLine}}", "{{console:import.skippedHead}}", "{{console:import.skippedMore}}".

Meaning: Existing items were skipped; the last notice represents additional hidden rows.

Checks: Inspect the skipped names and saved commands.

Next action: Keep existing items or preview again for deliberate replacement.

<a id="nothing-applied"></a>
## No confirmed changes

Messages: "{{console:import.nothingApplied}}".

Meaning: The result has no applied items.

Checks: Check skipped and failed rows as well as selection.

Next action: Resolve their diagnostics before retrying.

<a id="failed"></a>
## Commit has failed items

Messages: "{{console:import.failedHead}}".

Meaning: Some selected items could not be written.

Checks: Compare successful counts with failed item names and reasons.

Next action: Use the failed-items retry after resolving the cause.

<a id="failed-rejected"></a>
## Item rejected during write

Messages: "{{console:import.failedReason.rejected}}".

Meaning: The write service rejected this item.

Checks: Check the row validation and source data.

Next action: Correct the source or recreate that item manually.

<a id="failed-invalid"></a>
## Item invalid at commit

Messages: "{{console:import.failedReason.invalid}}".

Meaning: Validation prevented this row from being committed.

Checks: Inspect its diagnostic and field limits.

Next action: Fix the source data, preview again, and retry the affected item.

<a id="failed-module"></a>
## Required module unavailable

Messages: "{{console:import.failedReason.module}}".

Meaning: A module required to store the item was unavailable.

Checks: Check module loading and the affected timers, triggers, or terms.

Next action: Retry failed items after module data is available.

<a id="onboarding-save"></a>
## Import kept but setup unfinished

Messages: "{{console:onboardingImport.saveFailed}}".

Meaning: Onboarding finalization failed while retaining the import.

Checks: Check the imported items before running another import.

Next action: Use the dashboard control to retry finishing setup.

<a id="onboarding-signin"></a>
## Sign-in required to finish

Messages: "{{console:onboardingImport.signIn}}", "{{console:serverErrors.signInToFinish}}".

Meaning: The session ended before setup completion.

Checks: Check the current session.

Next action: Sign in again and finish setup.

<a id="onboarding-owner"></a>
## Only owner can finish setup

Messages: "{{console:onboardingImport.ownerOnly}}", "{{console:serverErrors.ownerFinishSetup}}".

Meaning: A delegate cannot finalize channel onboarding.

Checks: Check the Twitch identity.

Next action: Have the channel owner sign in and finish setup.

<a id="manifest-empty"></a>
## Empty manifest

Meaning: The manifest contains no counted items or moderation terms.

Checks: Diagnostic code: `manifest_empty`. Check published source data.

Next action: Export or publish supported items and preview again.

<a id="unsupported-source"></a>
## Unsupported source

Meaning: The request source is missing or unsupported.

Checks: Diagnostic code: `unsupported_source`. Check the selected source.

Next action: Restart using a supported source tile.

<a id="credential-required"></a>
## Required source input absent

Meaning: The parser has no required credential or public channel name.

Checks: Diagnostic code: `credential_required`. Check the selected source input.

Next action: Enter it only in the designated dashboard field.

<a id="file-required"></a>
## Required source file absent

Meaning: The server parser has no upload.

Checks: Diagnostic code: `file_required`. Check the export file selection.

Next action: Choose the correct export and preview.

<a id="file-decode-failed"></a>
## File decoding failed

Meaning: The uploaded file encoding could not be decoded.

Checks: Diagnostic code: `file_decode_failed`. Check that the original export was selected.

Next action: Export a fresh file and preview.

<a id="diagnostic-file-too-large"></a>
## Parser file limit exceeded

Meaning: The parser file-size limit was exceeded.

Checks: Diagnostic code: `file_too_large`. Check the source export size.

Next action: Reduce the source export before preview.

<a id="fetch-failed"></a>
## Source retrieval failed

Meaning: The importer could not retrieve source data.

Checks: Diagnostic code: `fetch_failed`. Check source connection, public directory availability, and diagnostic context.

Next action: Retry later or reconnect the source as applicable.

<a id="parse-failed"></a>
## Source parsing failed

Meaning: The source response or file could not be parsed.

Checks: Diagnostic code: `parse_failed`. Check source and export format.

Next action: Use a fresh supported export or retry the source lookup.

<a id="collision-lookup-failed"></a>
## Conflict lookup incomplete

Meaning: Existing names could not be read for collision checks.

Checks: Diagnostic code: `collision_lookup_failed`. Check that the commands board loads.

Next action: Reload before committing; do not assume the preview has found every conflict.

<a id="command-name-invalid"></a>
## Imported command name invalid

Meaning: The normalized name is not 1 through 64 printable ASCII characters without whitespace.

Checks: Diagnostic code: `command_name_invalid`. Inspect the name and normalization.

Next action: Fix it in the source and preview again.

<a id="command-alias-invalid"></a>
## Imported alias invalid

Meaning: An alias is invalid, duplicated, or beyond the manifest limit of 25 aliases; some parsers drop invalid aliases.

Checks: Diagnostic code: `command_alias_invalid`. Inspect aliases and diagnostic severity.

Next action: Fix the source alias or accept its removal deliberately.

<a id="command-response-invalid"></a>
## Imported response invalid

Meaning: A usable response is missing or violates line limits.

Checks: Diagnostic code: `command_response_invalid`. Check 1 through 5 non-empty lines, each at most 500 bytes, without control characters.

Next action: Fix source text and preview again.

<a id="command-response-truncated"></a>
## Response shortened

Meaning: An imported response line exceeded the byte limit and was cut.

Checks: Diagnostic code: `command_response_truncated`. Read the translated response, especially token endings.

Next action: Shorten the source text or repair the command after import.

<a id="command-response-line-dropped"></a>
## Extra response messages dropped

Meaning: Lines beyond the five-message limit were removed.

Checks: Diagnostic code: `command_response_line_dropped`. Compare source and preview line counts.

Next action: Choose the five intended messages before importing.

<a id="command-permission-unmapped"></a>
## Permission not mapped

Meaning: An upstream permission is unsupported; a parser may default it to everyone, while manifest validation can reject an invalid tier.

Checks: Diagnostic code: `command_permission_unmapped`. Read the source-specific diagnostic and resulting access.

Next action: Set a supported access tier before testing with viewers.

<a id="command-cooldown-clamped"></a>
## Cooldown bounded

Meaning: The cooldown is reduced to the maximum of 86400 seconds.

Checks: Diagnostic code: `command_cooldown_clamped`. Compare the intended cooldown with preview.

Next action: Accept the supported limit or revise the setting.

<a id="command-variable-unmapped"></a>
## Command variable has no translation

Meaning: A token has no supported equivalent and may remain literal.

Checks: Diagnostic code: `command_variable_unmapped`. Inspect every flagged token in the translated response.

Next action: Replace it with supported variables or ordinary text.

<a id="timer-interval-clamped"></a>
## Timer interval bounded

Meaning: The interval is constrained to supported bounds; commit uses a 30-second floor and seven-day ceiling.

Checks: Diagnostic code: `timer_interval_clamped`. Compare source interval with the resulting timer.

Next action: Choose a supported interval and verify it after import.

<a id="timer-gate-invalid"></a>
## Invalid timer activity gate

Meaning: A timer gate is not a finite non-negative number.

Checks: Diagnostic code: `timer_gate_invalid`. Inspect chat-line and chat-window values.

Next action: Fix the source gate and preview.

<a id="timer-gate-clamped"></a>
## Timer activity gate bounded

Meaning: Commit bounds minimum chat lines to 0 through 100 and chat-window minutes to 1 through 60.

Checks: Diagnostic code: `timer_gate_clamped`. Compare the source gate with supported bounds.

Next action: Accept the bounded gate or adjust the timer.

<a id="timer-message-empty"></a>
## Timer has no usable message

Meaning: No usable timer response remains after translation.

Checks: Diagnostic code: `timer_message_empty`. Inspect referenced command and timer text.

Next action: Add a supported message before previewing again.

<a id="trigger-invalid"></a>
## Keyword trigger invalid

Meaning: A trigger lacks phrase or response, or its phrase violates reserved-syntax checks.

Checks: Diagnostic code: `trigger_invalid`. Inspect phrase, response, and diagnostic detail.

Next action: Correct both fields and avoid reserved syntax.

<a id="quote-text-invalid"></a>
## Quote text invalid

Meaning: The quote is empty or exceeds 450 bytes.

Checks: Diagnostic code: `quote_text_invalid`. Inspect the exported quote text.

Next action: Use 1 through 450 bytes and preview again.

<a id="quote-date-invalid"></a>
## Quote date invalid

Meaning: The quote timestamp is not valid RFC 3339.

Checks: Diagnostic code: `quote_date_invalid`. Check date, time, and timezone.

Next action: Correct the source timestamp or omit it.

<a id="automod-terms-too-many"></a>
## Moderation term list capped

Meaning: Terms beyond the 200-entry per-list limit are dropped.

Checks: Diagnostic code: `automod_terms_too_many`. Inspect both block and allow lists.

Next action: Keep the intended terms within each list limit.

<a id="module-read-failed"></a>
## Module data unavailable

Meaning: Module data could not be read; dependent imports may be omitted or fail.

Checks: Diagnostic code: `module_read_failed`. Check timers, triggers, moderation terms, and actual result counts.

Next action: Retry affected items when module data returns.

<a id="write-failed"></a>
## Item write failed

Meaning: A selected item or module update failed to write.

Checks: Diagnostic code: `write_failed`. Inspect failed names and successful counts.

Next action: Retry only failed items after resolving the write error.

<a id="command-too-many"></a>
## Command collection exceeds import cap

Meaning: Only the permitted command prefix can be imported.

Checks: Diagnostic code: `command_too_many`. Inspect error rows past the command cap.

Next action: Split the source export or recreate omitted items manually.

<a id="timer-too-many"></a>
## Timer collection exceeds import cap

Meaning: Only the permitted timer prefix can be imported.

Checks: Diagnostic code: `timer_too_many`. Inspect rows past the timer cap.

Next action: Reduce the export and preview again.

<a id="trigger-too-many"></a>
## Trigger collection exceeds import cap

Meaning: Only the permitted trigger prefix can be imported.

Checks: Diagnostic code: `trigger_too_many`. Inspect rows past the trigger cap.

Next action: Reduce the export and preview again.

<a id="quote-too-many"></a>
## Quote collection exceeds import cap

Meaning: Only the permitted quote prefix can be imported.

Checks: Diagnostic code: `quote_too_many`. Inspect rows past the quote cap.

Next action: Reduce the export and preview again.

<a id="command-permission-widened"></a>
## Moobot access widened

Meaning: A source permission is mapped to a broader supported tier.

Checks: Diagnostic code: `command_permission_widened`. Compare source and imported access.

Next action: Restrict the imported command as required before viewer use.

<a id="command-permission-collapsed"></a>
## Moobot permission groups collapsed

Meaning: Multiple source permission groups became one supported tier.

Checks: Diagnostic code: `command_permission_collapsed`. Inspect resulting access.

Next action: Choose the intended supported tier explicitly.

<a id="command-fetch-url-absent"></a>
## Moobot fetched response unavailable

Meaning: A command needs source fetched-response behavior that has no usable imported URL.

Checks: Diagnostic code: `command_fetch_url_absent`. Inspect preview text and source command.

Next action: Configure a supported data source manually.

<a id="command-count-remapped"></a>
## Usage counter remapped

Meaning: Source counter behavior was converted to the destination counter mechanism.

Checks: Diagnostic code: `command_count_remapped`. Compare preview tokens and expected counts.

Next action: Verify counter behavior after import; do not assume source historical values were restored.

<a id="command-positional-fallback-lost"></a>
## Moobot positional fallback lost

Meaning: A source positional default cannot be retained.

Checks: Diagnostic code: `command_positional_fallback_lost`. Inspect flagged argument variables.

Next action: Rewrite the fallback with supported response syntax.

<a id="command-disabled"></a>
## Disabled source command skipped

Meaning: A disabled upstream command was not imported.

Checks: Diagnostic code: `command_disabled`. Check whether it should remain disabled.

Next action: Enable it in the source and preview if it should come over.

<a id="permission-groups-skipped"></a>
## Moobot permission groups skipped

Meaning: Custom permission group definitions were not imported.

Checks: Diagnostic code: `permission_groups_skipped`. Inspect dependent command access.

Next action: Configure supported command access manually.

<a id="response-overrides-skipped"></a>
## Moobot response overrides skipped

Meaning: Source response overrides were not imported.

Checks: Diagnostic code: `response_overrides_skipped`. Compare effective source responses with preview.

Next action: Recreate needed response text manually.

<a id="timer-disabled"></a>
## Disabled Moobot timer skipped

Meaning: An upstream timer is disabled.

Checks: Diagnostic code: `timer_disabled`. Check its source enable state.

Next action: Enable it upstream and preview if it is wanted.

<a id="timer-command-unresolved"></a>
## Moobot timer command unresolved

Meaning: A timer reference could not be resolved into imported text.

Checks: Diagnostic code: `timer_command_unresolved`. Inspect the referenced source command.

Next action: Give the timer a supported response or recreate it manually.

<a id="command-alias-unresolved"></a>
## Moobot alias target unresolved

Meaning: An alias points at a command unavailable to the import.

Checks: Diagnostic code: `command_alias_unresolved`. Check the source alias target.

Next action: Fix the target or add the alias manually after importing the command.

<a id="command-alias-arguments"></a>
## Moobot alias arguments dropped

Meaning: The alias carries source arguments that cannot be preserved as an alternate name.

Checks: Diagnostic code: `command_alias_arguments`. Compare original behavior with preview.

Next action: Create a separate custom response if fixed arguments are needed.

<a id="command-regular-widened"></a>
## Nightbot regular access widened

Meaning: Regular-only access became everyone because that tier has no equivalent.

Checks: Diagnostic code: `command_regular_widened`. Inspect resulting permissions.

Next action: Choose a supported restricted tier if everyone is too broad.

<a id="command-unparseable-skipped"></a>
## Unparseable command skipped

Meaning: A source command row could not be parsed.

Checks: Diagnostic code: `command_unparseable_skipped`. Check source record and required name/response fields.

Next action: Repair the source row and preview again.

<a id="timer-unparseable-skipped"></a>
## Unparseable timer skipped

Meaning: A source timer row could not be parsed.

Checks: Diagnostic code: `timer_unparseable_skipped`. Inspect interval and response fields.

Next action: Repair the source record and preview.

<a id="timer-disabled-skipped"></a>
## Disabled upstream timer skipped

Meaning: A disabled source timer was omitted.

Checks: Diagnostic code: `timer_disabled_skipped`. Check source enable state.

Next action: Enable it upstream only if it should be imported.

<a id="timer-variable-unmapped"></a>
## Timer variable not translated

Meaning: A timer token has no supported equivalent.

Checks: Diagnostic code: `timer_variable_unmapped`. Read its translated response.

Next action: Replace the token before relying on the timer.

<a id="timer-message-truncated"></a>
## Timer response shortened

Meaning: A timer response line was shortened to the supported byte limit.

Checks: Diagnostic code: `timer_message_truncated`. Read the full preview for cut text or tokens.

Next action: Shorten the source message or repair it after import.

<a id="timer-message-line-dropped"></a>
## Timer response lines dropped

Meaning: Timer messages beyond the line cap were omitted.

Checks: Diagnostic code: `timer_message_line_dropped`. Compare source and preview.

Next action: Retain only supported intended messages.

<a id="automod-term-regex-skipped"></a>
## Nightbot regex term skipped

Meaning: A regular-expression moderation term was omitted.

Checks: Diagnostic code: `automod_term_regex_skipped`. Inspect omitted terms.

Next action: Configure supported literal block or allow terms manually.

<a id="automod-terms-capped"></a>
## Nightbot terms capped

Meaning: Source moderation terms beyond the cap were omitted.

Checks: Diagnostic code: `automod_terms_capped`. Inspect retained block and allow terms.

Next action: Keep intended lists within supported limits.

<a id="command-regex-skipped"></a>
## StreamElements regex command skipped

Meaning: Regex-triggered command behavior is unsupported.

Checks: Diagnostic code: `command_regex_skipped`. Inspect source command matching mode.

Next action: Recreate an ordinary command or supported keyword trigger.

<a id="command-disabled-skipped"></a>
## StreamElements disabled command skipped

Meaning: A disabled command was omitted.

Checks: Diagnostic code: `command_disabled_skipped`. Check its upstream enable state.

Next action: Enable it upstream and preview if needed.

<a id="command-alias-dropped"></a>
## StreamElements alias dropped

Meaning: An alias could not be retained.

Checks: Diagnostic code: `command_alias_dropped`. Inspect the alias and its normalized form.

Next action: Fix the source alias or add a valid alias after import.

<a id="command-cost-unsupported"></a>
## StreamElements command cost lost

Meaning: The source point-cost behavior is unsupported in the imported command.

Checks: Diagnostic code: `command_cost_unsupported`. Check source cost and preview.

Next action: Do not treat the imported command as charging those points.

<a id="command-type-whisper"></a>
## Whisper converted to chat

Meaning: Source whisper behavior becomes ordinary chat output.

Checks: Diagnostic code: `command_type_whisper`. Review whether response text is suitable for public chat.

Next action: Rewrite or deselect it before importing.

<a id="command-type-reply"></a>
## Threaded reply converted to chat

Meaning: Source threaded reply behavior becomes ordinary chat output.

Checks: Diagnostic code: `command_type_reply`. Review expected reply placement.

Next action: Accept ordinary chat behavior or deselect the row.

<a id="command-type-unknown"></a>
## Unrecognized command delivery type

Meaning: An unknown source delivery type uses ordinary chat behavior.

Checks: Diagnostic code: `command_type_unknown`. Review source delivery settings and preview.

Next action: Confirm intended public output before importing.

<a id="command-offline-only-widened"></a>
## Offline-only command restriction lost

Meaning: An offline-only source restriction cannot be retained.

Checks: Diagnostic code: `command_offline_only_widened`. Check expected stream-state behavior.

Next action: Review the imported command before enabling it.

<a id="timer-offline-only-widened"></a>
## Offline-only timer restriction lost

Meaning: An offline-only source timer becomes eligible during live operation.

Checks: Diagnostic code: `timer_offline_only_widened`. Check desired stream-state behavior.

Next action: Reconfigure or omit the timer if this change is unsuitable.

<a id="timer-offline-interval-dropped"></a>
## Separate offline interval dropped

Meaning: The source offline interval is not retained.

Checks: Diagnostic code: `timer_offline_interval_dropped`. Compare source live and offline intervals.

Next action: Choose the intended supported timer interval.

<a id="trigger-invalid-skipped"></a>
## StreamElements keyword skipped

Meaning: An invalid keyword entry was omitted.

Checks: Diagnostic code: `trigger_invalid_skipped`. Inspect source phrase and response.

Next action: Repair the source keyword or create a supported trigger manually.

<a id="trigger-response-truncated"></a>
## Trigger response shortened

Meaning: A keyword response line was shortened.

Checks: Diagnostic code: `trigger_response_truncated`. Inspect preview text and variables.

Next action: Shorten the source text or repair the imported trigger.

<a id="trigger-response-line-dropped"></a>
## Trigger response lines dropped

Meaning: Extra keyword-response lines were removed.

Checks: Diagnostic code: `trigger_response_line_dropped`. Compare source and preview.

Next action: Choose messages that fit the supported line cap.

<a id="trigger-variable-unmapped"></a>
## Trigger variable not translated

Meaning: A keyword response token has no equivalent.

Checks: Diagnostic code: `trigger_variable_unmapped`. Inspect flagged variables.

Next action: Replace them with supported variables or ordinary text.

<a id="command-builtin-skipped"></a>
## Fossabot built-in skipped

Meaning: An upstream built-in is not imported as a custom command.

Checks: Diagnostic code: `command_builtin_skipped`. Check the destination built-in inventory.

Next action: Use the destination built-in or create a distinct custom command.

<a id="command-action-prefix-dropped"></a>
## Fossabot action prefix removed

Meaning: The source action-message prefix was removed.

Checks: Diagnostic code: `command_action_prefix_dropped`. Inspect resulting response formatting.

Next action: Accept ordinary chat text or adjust the response.

<a id="command-type-unsupported-skipped"></a>
## Wizebot non-text command skipped

Meaning: Unsupported animation or sound-only command behavior was omitted.

Checks: Diagnostic code: `command_type_unsupported_skipped`. Check source command type.

Next action: Create a text response manually if appropriate.

<a id="command-duplicate-skipped"></a>
## Wizebot duplicate skipped

Meaning: A duplicate normalized command was omitted.

Checks: Diagnostic code: `command_duplicate_skipped`. Compare source names and aliases.

Next action: Keep a unique intended command in the source.

<a id="command-response-empty"></a>
## Wizebot empty response skipped

Meaning: The public directory exposes no usable response.

Checks: Diagnostic code: `command_response_empty`. Check published description and text response.

Next action: Publish usable text or recreate the command manually.

<a id="command-cost-dropped"></a>
## Wizebot cost removed

Meaning: Upstream cost behavior was dropped.

Checks: Diagnostic code: `command_cost_dropped`. Inspect source command cost.

Next action: Do not assume the imported command charges source points.

<a id="command-sound-dropped"></a>
## Wizebot sound removed

Meaning: The source sound effect was not imported.

Checks: Diagnostic code: `command_sound_dropped`. Check source sound settings.

Next action: Verify that the remaining text is useful.

<a id="command-random-lines-flattened"></a>
## Wizebot random response flattened

Meaning: Random alternatives became ordinary response lines.

Checks: Diagnostic code: `command_random_lines_flattened`. Read the preview and number of lines.

Next action: Rewrite the command using supported behavior before testing.

<a id="manifest-source-note"></a>
## Desktop source limitations

Meaning: The database parser reports source-specific omissions.

Checks: Diagnostic code: `manifest_source_note`. Read the source limitation detail and compare the manifest.

Next action: Recreate omitted behavior manually.

<a id="command-permission-adjusted"></a>
## Desktop access adjusted

Meaning: An upstream access tier was translated to another supported tier.

Checks: Diagnostic code: `command_permission_adjusted`. Inspect the resulting permission.

Next action: Choose the intended access before enabling the command.

<a id="command-script-dependent"></a>
## Desktop script dependency

Meaning: The command depends on a source script that cannot run here.

Checks: Diagnostic code: `command_script_dependent`. Inspect preview response and source script behavior.

Next action: Recreate a supported text response or deselect it.

<a id="quote-date-unparsed"></a>
## Desktop quote date omitted

Meaning: The source quote date could not be parsed.

Checks: Diagnostic code: `quote_date_unparsed`. Check quote text and timestamp in the result.

Next action: Correct the date manually if required.

<a id="command-positional-lower-lost"></a>
## Desktop lowercase argument behavior lost

Meaning: A source positional transformation cannot be retained.

Checks: Diagnostic code: `command_positional_lower_lost`. Inspect affected argument tokens.

Next action: Rewrite the response with supported syntax.

<a id="command-positional-numeric-lost"></a>
## Desktop numeric argument behavior lost

Meaning: A source numeric-only positional transformation cannot be retained.

Checks: Diagnostic code: `command_positional_numeric_lost`. Inspect affected argument tokens.

Next action: Rewrite the response before relying on numeric input filtering.

<a id="fetch-def-collision"></a>
## Imported data source name conflict

Meaning: A synthesized fetch definition collides with an existing name.

Checks: Diagnostic code: `fetch_def_collision`. Inspect the emitted definition name and affected command tokens.

Next action: Resolve the data source name and verify references manually.

<a id="fetch-def-created"></a>
## Data source synthesized

Meaning: The importer created a fetch definition from a source variable; this is an advisory.

Checks: Diagnostic code: `fetch_def_created`. Inspect its URL, path, and affected command references.

Next action: Verify the data source in the dashboard before relying on live results.

<a id="manifest-commands-capped"></a>
## Parsed commands capped

Meaning: The parser dropped commands beyond its per-import cap.

Checks: Diagnostic code: `manifest_commands_capped`. Compare source and preview counts.

Next action: Reduce or split the export and recreate omitted items as needed.

<a id="manifest-timers-capped"></a>
## Parsed timers capped

Meaning: The parser dropped timers beyond its per-import cap.

Checks: Diagnostic code: `manifest_timers_capped`. Compare source and preview counts.

Next action: Reduce or split the export and recreate omitted items as needed.

<a id="manifest-triggers-capped"></a>
## Parsed triggers capped

Meaning: The parser dropped triggers beyond its per-import cap.

Checks: Diagnostic code: `manifest_triggers_capped`. Compare source and preview counts.

Next action: Reduce or split the export and recreate omitted items as needed.

<a id="manifest-quotes-capped"></a>
## Parsed quotes capped

Meaning: The parser dropped quotes beyond its per-import cap.

Checks: Diagnostic code: `manifest_quotes_capped`. Compare source and preview counts.

Next action: Reduce or split the export and recreate omitted items as needed.

<a id="manifest-fetches-capped"></a>
## Parsed fetches capped

Meaning: The parser dropped fetches beyond its per-import cap.

Checks: Diagnostic code: `manifest_fetches_capped`. Compare source and preview counts.

Next action: Reduce or split the export and recreate omitted items as needed.

<related>

- [Import setup](../setup/import.md).
- [Migration reference](../migration.md).
- [Command errors](commands.md).

</related>

</page>
