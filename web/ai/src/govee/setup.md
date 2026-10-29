---
title: Setup reference for AI assistants: {{console:setupAi.featureGovee}}
description: Four dashboard steps to get a Govee API key from the Govee Home app, paste it, and see your lights. Read this first.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/guides/govee-lights/
---

[Human guide](https://itsbagelbot.com/guides/govee-lights/).

Get a Govee API key from Govee Home, save it in the dashboard, and load your colour lights.
Then create a channel-points reward for each light viewers can control.

<page type="setup">

<audience>

The user is in the {{console:common.appName}} dashboard, running the Govee setup ("{{console:goveeSetup.regionLabel}}"). It has four steps, numbered 01 to 04 in the step indicator at the top right: "{{console:goveeSetup.railNeed}}", "{{console:goveeSetup.railKey}}", "{{console:goveeSetup.railPaste}}", "{{console:goveeSetup.railLights}}". {{console:setupAi.guideStep}}

</audience>

<assistant_rules>

- Present one step at a time. Wait for the user to confirm or describe the result before continuing.
- {{console:setupAi.guideControls}}
- Do not request, accept, or output the Govee API key. Direct the user to paste it only into the dashboard field "{{console:govee.keyFieldLabel}}". If a key is pasted into chat, tell the user to apply for a new key in the Govee Home app and paste only the new one into the dashboard.
- A new Govee key deactivates older keys, so suggest a new key only when needed.
- If an error appears, match it against [Govee errors](https://ai.itsbagelbot.com/govee/errors.md) and follow that section.
- The Govee Home app changes. If a menu label differs, tell the user to look for the closest match and describe what they see.
- Follow only the buttons, fields, and limits documented on this page. If a setting is not covered, say so.
- {{console:setupAi.guideLanguage}}
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

## Before you start

- A Govee Home account.
- Govee lights already set up in the Govee Home app. The dashboard says: {{console:goveeSetup.needNoteBody}}
- Only lights that can change colour and connect to Wi-Fi are used. Bluetooth-only lights have no API access.

</facts>

<a id="step-1"></a>
## Step 1: {{console:goveeSetup.railNeed}}

Step name: "{{console:goveeSetup.railNeed}}". Page title: "{{console:goveeSetup.needTitle}}"

The page shows the note "{{console:goveeSetup.needNoteTitle}}".

1. Ask the user to confirm the requirements in "Before you start".
2. Ask the user to select "{{console:goveeSetup.start}}".

How to tell it worked: the step indicator moves to the next number ("{{console:goveeSetup.railKey}}") and the title reads "{{console:goveeSetup.keyTitle}}"

Common problems at this step:

- Lights are missing later: see [No Govee devices found](errors.md#no-devices).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-2"></a>
## Step 2: {{console:goveeSetup.railKey}}

Step name: "{{console:goveeSetup.railKey}}". Page title: "{{console:goveeSetup.keyTitle}}"

The page says: {{console:goveeSetup.keyBody}} Under "{{console:goveeSetup.pathLabel}}" it shows: {{console:govee.keyPath}}

In the Govee Home app on the phone:

1. Open the app and sign in to the account that owns the lights.
2. Open Settings. The dashboard path starts at the profile. The official Govee path is Settings, then "Apply for API Key". On some app versions the entry is under "About us". If the user cannot find it, ask what menus they see.
3. Select "Apply for API Key".
4. Fill in the short form and submit it. The form asks for a name and a reason. Accept the terms if asked.
5. Wait for the key. Govee decides when to approve. If it does not arrive after a while, apply again.

Tell the user both notes the dashboard shows:

- {{console:goveeSetup.keyEmailHint}}
- {{console:goveeSetup.keyOlderHint}} If another tool uses an older key, it stops working.

In the dashboard, when the key has arrived:

1. Select "{{console:goveeSetup.haveKey}}".

How to tell it worked: the step indicator moves to the next number ("{{console:goveeSetup.railPaste}}") and the title reads "{{console:goveeSetup.pasteTitle}}"

Common problems at this step:

- Key never arrives: check spam, then apply again.
- Older key stopped working: see [Govee no longer accepts the key](errors.md#key-rejected).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-3"></a>
## Step 3: {{console:goveeSetup.railPaste}}

Step name: "{{console:goveeSetup.railPaste}}". Page title: "{{console:goveeSetup.pasteTitle}}"

The page says: {{console:goveeSetup.pasteBody}}

1. Copy the key from the Govee email.
2. Paste it into "{{console:govee.keyFieldLabel}}" in the dashboard. Do not paste it into the AI chat. The dashboard says: {{console:goveeSetup.keyStored}}
3. Paste with no extra spaces.
4. Select "{{console:goveeSetup.saveContinue}}". The dashboard checks the key with Govee first.

How to tell it worked: the step indicator moves to the next number ("{{console:goveeSetup.railLights}}"). If the page also shows "{{console:govee.keyUnchecked}}", the key is saved but not checked yet. The user can continue.

Common problems at this step:

- Empty field: see [Enter your Govee API key](errors.md#key-empty).
- Govee refused the key: see [Govee did not accept the key](errors.md#key-invalid).
- Govee did not answer: see [Key saved but not checked](errors.md#key-unchecked).
- Save failed: see [Could not save the key](errors.md#key-save-failed).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-4"></a>
## Step 4: {{console:goveeSetup.railLights}}

Step name: "{{console:goveeSetup.railLights}}". Page title: "{{console:goveeSetup.lightsTitle}}"

The page says: {{console:goveeSetup.lightsBody}}

1. Wait for the list to load ("{{console:govee.loadingLights}}").
2. The page shows the number of colour lights found and their names. The text has the form "{{console:goveeSetup.lightsFound}}", with n replaced by the count.
3. Select "{{console:goveeSetup.readyCta}}" to go to the "{{console:modules.catalog.govee.label}}" page.

On the "{{console:modules.catalog.govee.label}}" page the user turns on "{{console:govee.masterLabel}}" and creates one reward per light. Viewers type a colour when they redeem it. "{{console:govee.liveOnlyLabel}}" is on by default, so redemptions work only while the stream is live.

How to tell it worked: at least one light is listed under "{{console:govee.lightsTitle}}", and the page shows "{{console:govee.keyOnFile}}".

Common problems at this step:

- Govee rejected the saved key: see [Govee no longer accepts the key](errors.md#key-rejected).
- Could not reach Govee: see [Could not reach Govee](errors.md#devices-error).
- No devices: see [No Govee devices found](errors.md#no-devices).
- No colour lights: see [No colour-capable lights](errors.md#no-lights).


Ask the user to confirm completion or describe what they see before continuing.

<related>

- [Feature reference](../features/govee.md).
- [Setup](setup.md).
- [Errors](errors.md).
- [Human guide](https://itsbagelbot.com/guides/govee-lights/).
- [Support](https://itsbagelbot.com/contact/) and [community](https://discord.gg/SZ2remwSDv).

</related>

</page>
