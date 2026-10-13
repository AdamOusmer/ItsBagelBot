---
title: Custom command errors: reference for AI assistants
description: Validation, naming conflicts, editor warnings, and failed custom-command changes with checks and recovery actions.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/commands/
---

[Human guide](https://itsbagelbot.com/guides/commands/).

Validation, naming conflicts, editor warnings, and failed custom-command changes with checks and recovery actions.

<page type="errors">

<audience>

Assistants explaining custom-command editor notices to a broadcaster or a delegate with command-management access.

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

- Dashboard: [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands). Custom commands are available on {{website:pricingTiers.free}}.
- Names and aliases allow at most 64 characters without whitespace or an embedded `!`. Normalization removes leading `!` and lowercases names.
- Responses allow 5 non-empty lines, at most 500 characters per line. A response can reference at most 3 different fetched values.
- Shared and per-viewer cooldowns are whole seconds from 0 through 86400. User restrictions take numeric Twitch IDs.
- `{name}`, `{max}`, `{n}`, and other variables in message placeholders are replaced by runtime values.
- Data source validation and sample notices can appear inside the command editor. Source names allow 32 characters, URLs 512, JSON paths eight segments, and channels 20 definitions.
- Counter-name validation currently has no locale message key. Its three checks are described by field and condition below without quoting those notices.

</facts>

<a id="name-required"></a>
## Empty command name

Messages: "{{console:validation.commandNameRequired}}".

Meaning: The normalized name is empty.

Checks: Inspect the name field after removing any leading `!`.

Next action: Enter a name and save.

<a id="name-length"></a>
## Command name too long

Messages: "{{console:validation.commandNameMax}}".

Meaning: The name exceeds 64 characters.

Checks: Count the stored name without the chat prefix.

Next action: Shorten it to at most 64 characters.

<a id="name-spaces"></a>
## Whitespace in command name

Messages: "{{console:validation.commandNameSpaces}}".

Meaning: The name contains whitespace.

Checks: Look for spaces, tabs, or pasted line breaks.

Next action: Remove whitespace from the trigger.

<a id="name-bang"></a>
## Exclamation mark in stored name

Messages: "{{console:validation.commandNameBang}}".

Meaning: An exclamation mark remains in the stored name.

Checks: Inspect the name after leading-prefix normalization.

Next action: Use `!` only when typing the trigger in chat.

<a id="alias-required"></a>
## Empty alternate name

Messages: "{{console:validation.alternateNameRequired}}".

Meaning: An alternate-name entry is empty.

Checks: Inspect each alternate-name entry.

Next action: Remove the empty entry or enter a name.

<a id="alias-length"></a>
## Alternate name too long

Messages: "{{console:validation.alternateNameMax}}".

Meaning: An alias exceeds 64 characters.

Checks: Inspect the alias identified by the notice.

Next action: Shorten it to at most 64 characters.

<a id="alias-spaces"></a>
## Whitespace in alternate name

Messages: "{{console:validation.alternateNameSpaces}}".

Meaning: An alias contains whitespace.

Checks: Inspect the named alias for pasted whitespace.

Next action: Remove the whitespace.

<a id="alias-bang"></a>
## Exclamation mark in alternate name

Messages: "{{console:validation.alternateNameBang}}".

Meaning: An alias contains an exclamation mark after normalization.

Checks: Inspect the named alias.

Next action: Remove the exclamation mark from the stored alias.

<a id="alias-own-name"></a>
## Alias repeats the command name

Messages: "{{console:validation.aliasOwnName}}".

Meaning: An alias equals the normalized primary name.

Checks: Compare the name and aliases case-insensitively.

Next action: Remove the redundant alias.

<a id="alias-duplicate"></a>
## Duplicate alternate name

Messages: "{{console:validation.aliasDuplicate}}".

Meaning: An alias repeats an earlier alias.

Checks: Inspect all normalized aliases.

Next action: Keep one occurrence.

<a id="response-required"></a>
## Empty response

Messages: "{{console:validation.responseRequired}}".

Meaning: No non-empty response line remains.

Checks: Check the response after blank lines and trailing whitespace are removed.

Next action: Write at least one non-empty response line.

<a id="response-lines"></a>
## Too many response lines

Messages: "{{console:validation.responseLinesMax}}".

Meaning: The response has more than 5 non-empty lines.

Checks: Count separate messages in the editor.

Next action: Reduce the response to at most 5 lines.

<a id="response-length"></a>
## Response line too long

Messages: "{{console:validation.responseCharsMax}}".

Meaning: A response line exceeds 500 characters.

Checks: Check each line rather than the total response length.

Next action: Shorten the affected line to at most 500 characters.

<a id="response-control"></a>
## Control characters in response

Messages: "{{console:validation.responseControlChars}}".

Meaning: A line includes a control character.

Checks: Inspect pasted text for tabs or invisible control characters.

Next action: Replace the affected text with ordinary text and save.

<a id="response-fetches"></a>
## Too many fetched values

Messages: "{{console:validation.responseFetchMax}}".

Meaning: The response references more than 3 different fetch names.

Checks: Count distinct `{urlfetch:name}` names; repeating the same one does not add another name.

Next action: Reduce distinct fetch references to at most 3.

<a id="cooldown-range"></a>
## Cooldown outside the range

Messages: "{{console:validation.cooldownRange}}".

Meaning: A cooldown is outside 0 through 86400 seconds or is not a valid number.

Checks: Check both shared and per-viewer cooldown fields.

Next action: Enter a whole number within that range.

<a id="cooldown-integer"></a>
## Fractional cooldown

Messages: "{{console:validation.cooldownInteger}}".

Meaning: A cooldown is not a whole number.

Checks: Check both cooldown fields for decimals.

Next action: Use whole seconds.

<a id="user-id"></a>
## Invalid user restriction

Messages: "{{console:validation.userIdNumeric}}".

Meaning: The user restriction contains something other than digits.

Checks: Check that the value is a Twitch user ID rather than a username.

Next action: Enter the numeric ID or clear the optional restriction.

<a id="builtin-name"></a>
## Reserved built-in name

Messages: "{{console:commands.errBuiltinName}}".

Meaning: The primary name is reserved by a built-in command.

Checks: Check the built-in commands and aliases.

Next action: Choose a different custom trigger.

<a id="name-taken"></a>
## Existing custom command name

Messages: "{{console:commands.errNameTaken}}".

Meaning: Another command already uses the name.

Checks: Check the intended name and the existing command.

Next action: Open the existing command or choose another name.

<a id="alias-taken"></a>
## Alias conflicts with another command

Messages: "{{console:commands.errAliasTaken}}".

Meaning: An alias is reserved or already used by a different command.

Checks: Compare aliases with built-in and custom names and aliases.

Next action: Remove or rename the conflicting alias.

<a id="unavailable"></a>
## Existing commands could not be checked

Messages: "{{console:commands.errUnavailable}}".

Meaning: The dashboard could not complete its collision check.

Checks: Check connection and whether the command board loads.

Next action: Wait, reload, and retry after existing commands can be read.

<a id="save-failed"></a>
## Command save failed

Messages: "{{console:commands.toastSaveFailed}}".

Meaning: The editor did not confirm a successful save.

Checks: Preserve the draft and inspect the saved command after reloading.

Next action: Retry the intended edit once the dashboard is reachable.

<a id="toggle-failed"></a>
## Command toggle failed

Messages: "{{console:commands.toastToggleFailed}}".

Meaning: The enable-state change failed.

Checks: Reload and check the actual active state.

Next action: Retry the switch if the intended state was not saved.

<a id="restore-failed"></a>
## Undo could not restore a command

Messages: "{{console:commands.toastCouldNotRestore}}".

Meaning: The restore operation failed.

Checks: Check whether the command is present and retain its draft text.

Next action: Recreate it manually if it remains missing.

<a id="delete-failed"></a>
## Command deletion failed

Messages: "{{console:commands.toastDeleteFailed}}".

Meaning: The dashboard did not confirm deletion.

Checks: Reload and check whether the named command remains.

Next action: Retry deletion only if it is still present and removal is intended.

<a id="bulk-partial"></a>
## Some bulk changes failed

Messages: "{{console:commands.bulkPartial}}".

Meaning: Only part of the selected operation succeeded.

Checks: Inspect the rows put back by the dashboard.

Next action: Retry only the failed rows.

<a id="bulk-failed"></a>
## Bulk operation failed

Messages: "{{console:commands.bulkFailed}}".

Meaning: The bulk operation failed without confirmed changes.

Checks: Check the selected rows and their saved states.

Next action: Retry the desired operation after reloading.

<a id="degraded"></a>
## Command list unavailable

Messages: "{{console:commands.degraded}}".

Meaning: Live custom-command data could not be loaded.

Checks: Check connectivity; an empty board in this state does not establish that commands were deleted.

Next action: Wait and reload before recreating commands.

<a id="line-over"></a>
## Editor line exceeds the limit

Messages: "{{console:commandEditor.lineOver}}".

Meaning: The indicated response line is over its limit.

Checks: Inspect its displayed character count; the maximum is 500.

Next action: Shorten that line.

<a id="line-near"></a>
## Editor line approaches the limit

Messages: "{{console:commandEditor.lineNear}}".

Meaning: The line is near the limit; this is an advisory.

Checks: Check the displayed count against 500.

Next action: Keep the final line within the limit.

<a id="lines-capped"></a>
## Cannot add another response line

Messages: "{{console:commandEditor.linesCapped}}".

Meaning: The editor already contains its maximum number of messages.

Checks: Count the response lines; the maximum is 5.

Next action: Remove a line before adding another.

<a id="replace-existing"></a>
## Creation would replace existing settings

Messages: "{{console:commands.composeReplace}}".

Meaning: The compose flow found an existing command with this name.

Checks: Review the existing response and settings before proceeding.

Next action: Open it for a deliberate edit or choose a new name.

<a id="signed-out"></a>
## Session ended

Messages: "{{console:serverErrors.notSignedIn}}".

Meaning: There is no active dashboard session for the action.

Checks: Check the signed-in channel identity.

Next action: Sign in again and reopen the command.

<a id="invalid-bulk"></a>
## Invalid bulk selection

Messages: "{{console:serverErrors.invalidInput}}".

Meaning: The bulk operation or selection is invalid.

Checks: Check that commands are selected and the action comes from the current board.

Next action: Reload and select the intended commands again.

<a id="counter-length"></a>
## Counter name too long

Meaning: The optional counter name exceeds 64 characters after its leading prefix is removed.

Checks: Inspect the counter field.

Next action: Shorten the name to at most 64 characters or clear it.

<a id="counter-spaces"></a>
## Whitespace in counter name

Meaning: The optional counter name contains whitespace.

Checks: Inspect the counter name for spaces or pasted whitespace.

Next action: Use a name without whitespace or clear the field.

<a id="counter-colon"></a>
## Colon in counter name

Meaning: The optional counter name contains a colon.

Checks: Inspect the counter field for `:`.

Next action: Remove the colon or clear the field.

<a id="fetch-name-required"></a>
## Data source name missing

Messages: "{{console:validation.definitionNameRequired}}", "{{console:fetches.errNameRequired}}".

Meaning: A fetched-value source has no stored name.

Checks: Check the name in the command editor data source builder.

Next action: Enter a source name.

<a id="fetch-name-length"></a>
## Data source name too long

Messages: "{{console:validation.definitionNameMax}}".

Meaning: The source name exceeds 32 characters.

Checks: Check the stored name used in the fetch token.

Next action: Shorten it to at most 32 characters.

<a id="fetch-name-charset"></a>
## Data source name characters invalid

Messages: "{{console:validation.definitionNameCharset}}".

Meaning: The source name contains unsupported characters.

Checks: Check for characters other than lowercase letters, digits, and underscores.

Next action: Use supported characters in the name.

<a id="fetch-url-required"></a>
## Data source URL missing

Messages: "{{console:validation.urlRequired}}", "{{console:fetches.errUrlRequired}}".

Meaning: The source has no API address.

Checks: Check the URL field.

Next action: Enter the public HTTPS API URL.

<a id="fetch-url-length"></a>
## Data source URL too long

Messages: "{{console:validation.urlMax}}".

Meaning: The API URL exceeds 512 characters.

Checks: Check the complete stored address.

Next action: Use a supported URL within 512 characters.

<a id="fetch-url-https"></a>
## Data source URL invalid

Messages: "{{console:validation.urlHttps}}".

Meaning: The URL is malformed or does not use HTTPS.

Checks: Check scheme and hostname.

Next action: Use a valid HTTPS URL.

<a id="fetch-url-public"></a>
## Data source host disallowed

Messages: "{{console:validation.urlPublic}}".

Meaning: The URL targets a disallowed local host or IP literal.

Checks: Check for localhost, local/internal suffixes, or an IP address.

Next action: Use the public HTTPS hostname of the intended API.

<a id="fetch-kind"></a>
## Data source response type invalid

Messages: "{{console:validation.fetchKind}}".

Meaning: The response kind is unsupported.

Checks: Check the source response kind.

Next action: Choose plain text or JSON.

<a id="fetch-plain-path"></a>
## Plain response has a JSON path

Messages: "{{console:validation.plainPath}}".

Meaning: Plain text fetches read the whole body and cannot use a field path.

Checks: Inspect response kind and path.

Next action: Clear the path or switch to JSON.

<a id="fetch-path-depth"></a>
## JSON field too deep

Messages: "{{console:validation.pathDepth}}", "{{console:fetches.pickerTooDeep}}".

Meaning: The chosen JSON path exceeds eight segments.

Checks: Inspect the selected field depth.

Next action: Choose a field closer to the root.

<a id="fetch-path-segment"></a>
## JSON path segment invalid

Messages: "{{console:validation.pathSegment}}".

Meaning: A path segment contains unsupported characters.

Checks: Check each segment for letters, digits, hyphens, or underscores only.

Next action: Choose a supported field path.

<a id="fetch-key-label"></a>
## Data source key label too long

Messages: "{{console:validation.keyLabelMax}}".

Meaning: The selected key label exceeds 32 characters.

Checks: Inspect the label rather than the secret value.

Next action: Use a stored key with a supported label.

<a id="fetch-name-taken"></a>
## Data source name already used

Messages: "{{console:validation.fetchExists}}", "{{console:fetches.errNameTaken}}".

Meaning: A source with this name already exists.

Checks: Inspect existing definitions and command references.

Next action: Use the existing source or choose a unique name.

<a id="fetch-limit"></a>
## Data source quota reached

Messages: "{{console:validation.fetchLimit}}", "{{console:fetches.quotaReached}}".

Meaning: The channel has reached the 20-source cap.

Checks: Inspect definitions and their command references.

Next action: Reuse a source or remove an unneeded one before adding another.

<a id="fetch-save"></a>
## Data source save failed

Messages: "{{console:fetches.toastSaveFailed}}", "{{console:fetches.saveFailedShort}}".

Meaning: The source was not confirmed saved.

Checks: Reload and inspect the actual definition.

Next action: Retry the intended definition change.

<a id="fetch-toggle"></a>
## Data source toggle failed

Messages: "{{console:fetches.toastToggleFailed}}".

Meaning: The source active-state update failed.

Checks: Check the stored active state.

Next action: Retry after reloading.

<a id="fetch-delete"></a>
## Data source deletion failed

Messages: "{{console:fetches.toastDeleteFailed}}".

Meaning: The source deletion was not confirmed.

Checks: Check whether it still exists and which commands reference it.

Next action: Retry only if removal is still intended.

<a id="fetch-sample-unreadable"></a>
## Sample response cannot be read automatically

Messages: "{{console:fetches.builderNoSample}}".

Meaning: The API answered but automatic sample reading failed.

Checks: Inspect a suitable response locally, without sharing secrets.

Next action: Paste a suitable sample into the dashboard builder.

<a id="fetch-sample-size"></a>
## Sample response too large

Messages: "{{console:fetches.pickerTooBig}}".

Meaning: The pasted sample exceeds the displayed size limit.

Checks: Check the limit in the notice.

Next action: Paste a smaller response slice.

<a id="fetch-sample-json"></a>
## Sample response is not JSON

Messages: "{{console:fetches.pickerBadJson}}".

Meaning: The sample could not be parsed as JSON.

Checks: Check brackets, quoting, and the intended response format.

Next action: Correct the JSON or use the appropriate plain response kind.

<a id="fetch-sample-fields"></a>
## Sample has too many fields

Messages: "{{console:fetches.pickerHuge}}".

Meaning: The sample expands into too many selectable fields.

Checks: Inspect the sample structure.

Next action: Paste a smaller slice.

<a id="fetch-picker-name"></a>
## Value picker needs a source name

Messages: "{{console:fetches.pickerNeedsName}}".

Meaning: A token cannot be constructed without a source name.

Checks: Check the source name.

Next action: Name the definition before selecting a value.

<a id="fetch-test-service"></a>
## Fetch test service unavailable

Messages: "{{console:fetches.testNoAnswer}}", "{{console:serverErrors.fetchService}}".

Meaning: The fetch service did not answer.

Checks: Check whether the source is saved and the dashboard reachable.

Next action: Wait and retry the test.

<a id="fetch-test-rate"></a>
## Too many sample tests

Messages: "{{console:serverErrors.fetchRate}}".

Meaning: Real API test requests are being made too quickly.

Checks: Stop repeated sample requests.

Next action: Wait about 10 seconds and test again.

<a id="fetch-test-fields"></a>
## Test blocked by invalid fields

Messages: "{{console:serverErrors.fixFields}}".

Meaning: Validation prevents the test request.

Checks: Inspect highlighted URL, kind, path, and key-label fields.

Next action: Correct the fields and test again.

<a id="fetch-test-denied"></a>
## API denied the sample request

Messages: "{{console:fetches.test_denied}}".

Meaning: The target API refused the request.

Checks: Check URL and selected stored API-key label.

Next action: Correct the URL or select the appropriate key in the dashboard.

<a id="fetch-test-limited"></a>
## API limited the sample request

Messages: "{{console:fetches.test_limited}}".

Meaning: The target API is rate limiting the test.

Checks: Avoid repeated requests.

Next action: Wait a moment and test again.

<a id="fetch-test-upstream"></a>
## API returned an error

Messages: "{{console:fetches.test_upstream_error}}".

Meaning: The API returned an error result.

Checks: Check source URL and any configured token fallback.

Next action: Retry later or configure a fallback for missing live values.

<a id="fetch-test-timeout"></a>
## API sample request timed out

Messages: "{{console:fetches.test_timeout}}".

Meaning: The API did not respond before the deadline.

Checks: Check the intended endpoint and token fallback.

Next action: Retry later or provide a fallback.

<a id="fetch-test-definition"></a>
## Source produced no value

Messages: "{{console:fetches.test_bad_def}}".

Meaning: The source cannot currently produce the selected value.

Checks: Check URL, selected key label, and field path.

Next action: Fetch a new sample and choose an available value; review token fallback behavior.

<related>

- [Commands setup](../setup/commands.md).
- [Commands feature](../features/commands.md).
- [Connection errors](connection.md).

</related>

</page>
