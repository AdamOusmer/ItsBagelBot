---
title: Fix {{console:setupAi.featureGovee}} errors
description: Every message the Govee setup, the Govee Lights page and chat can show, what it means, and what the user should do.
updated: 2026-10-12
---

## For assistants

Find the section whose message matches what the user sees. {{console:setupAi.guideLanguage}} In chat messages, `@name` stands for the viewer's name.

Rules:

- Give one step at a time. Wait for the user to confirm.
- Never ask for or accept a Govee API key.
- {{console:setupAi.guideControls}} Setup steps are in https://ai.itsbagelbot.com/govee/setup.md.
- Govee answers a bad key with HTTP 401. Any message about a rejected key means this.
- If two tries fail, send the user to https://itsbagelbot.com/contact/ or the Discord https://discord.gg/SZ2remwSDv.

## Messages in the dashboard

<a id="generic-error"></a>
### {{console:setupAi.genericError}}

Message: "{{console:setupAi.genericError}}"

{{console:setupAi.guideGenericError}}

<a id="key-empty"></a>
### Enter your Govee API key

Message: "{{console:serverErrors.goveeKey}}"

Meaning: the key field was empty.

Tell the user to paste the key from the Govee email into "{{console:govee.keyFieldLabel}}", then select "{{console:goveeSetup.saveContinue}}".

<a id="key-invalid"></a>
### Govee did not accept the key

Message: "{{console:govee.keyInvalid}}"

Meaning: Govee refused the key. It was not saved.

Tell the user:

1. Copy the key again from the Govee email, with no extra spaces.
2. If they applied for a newer key since, use the newest one. A new key turns off older keys.
3. Paste it and select "{{console:goveeSetup.saveContinue}}".
4. If it still fails, apply for a new key in the Govee Home app and paste that one.

<a id="key-unchecked"></a>
### Key saved but not checked

Message: "{{console:govee.keyUnchecked}}"

Meaning: the key is saved, but Govee did not answer, so it was not checked.

Tell the user to continue. If the lights do not change later, paste the key again.

<a id="key-save-failed"></a>
### Could not save the key

Message: "{{console:govee.keySaveFailed}}"

Tell the user to try again in a moment.

<a id="key-rejected"></a>
### Govee no longer accepts the key

Message: "{{console:govee.devicesKeyRejected}}"

Meaning: the saved key now fails with Govee. A common cause is a newer key on the same Govee account, which turns older keys off.

Tell the user:

1. Select "{{console:govee.keyRemove}}" on the Govee Lights page and confirm.
2. In the Govee Home app, apply for a new key.
3. Paste it into "{{console:govee.keyFieldLabel}}" and select "{{console:govee.keySave}}".

<a id="key-remove-failed"></a>
### Could not remove the key

Message: "{{console:govee.keyRemoveFailed}}"

Tell the user to wait a moment and select "{{console:govee.keyRemove}}" again.

<a id="devices-error"></a>
### Could not reach Govee

Message: "{{console:govee.devicesError}}"

Meaning: the dashboard could not get the light list. The key is still saved. Govee limits how often the list can be loaded, so repeated reloads can also trigger this.

Tell the user to wait a minute and select "{{console:govee.devicesRetry}}". If it keeps failing, see [Govee no longer accepts the key](#key-rejected).

<a id="no-devices"></a>
### No Govee devices found

Message: "{{console:govee.noDevices}}"

Meaning: Govee returned no devices for this key. The lights may not be on this Govee account. Lights that only use Bluetooth may not show up.

The page says: {{console:govee.noDevicesBody}}

Tell the user:

1. Open the Govee Home app and check that the lights are on the same account the key came from.
2. Connect the lights to Wi-Fi.
3. Select "{{console:govee.noLightsCta}}" in the dashboard.

<a id="no-lights"></a>
### No colour-capable lights

Message: "{{console:govee.noLights}}"

Meaning: devices exist, but none can change colour.

The page says: {{console:govee.noLightsBody}}

Tell the user to add a colour light to the Govee account, then select "{{console:govee.noLightsCta}}".

<a id="degraded"></a>
### Backend unreachable

Message: "{{console:govee.degraded}}"

Tell the user to wait a moment and reload the page.

<a id="reconnect"></a>
### Twitch reconnect needed

Message: "{{console:govee.reconnect}}"

Meaning: the bot needs channel-points access to create rewards.

Tell the user to select "{{console:govee.reconnectCta}}" and approve the access on Twitch.

<a id="toggle-failed"></a>
### Could not toggle Govee lights

Message: "{{console:govee.masterFail}}"

Tell the user to reload the page, check the "{{console:govee.masterLabel}}" switch, and try again.

<a id="reward-fields"></a>
### Reward field errors

Messages:

- "{{console:govee.errTitleRequired}}": type a reward title.
- "{{console:govee.errCost}}": enter a whole number of points, 1 or more.
- "{{console:govee.errCooldown}}": enter a cooldown from 0 seconds (off) up to 7 days.
- "{{console:serverErrors.rewardTitle}}": the title is required and has a length limit.

<a id="reward-save-failed"></a>
### Could not save or delete the reward

Messages: "{{console:govee.toastSaveFailed}}" and "{{console:govee.toastDeleteFailed}}"

Tell the user to wait a moment and try again. If saving keeps failing, they should check the title and cost, and that the channel can use channel points rewards on Twitch.

## Messages viewers see in chat

Each line below means the bot refunded the viewer's points.

<a id="chat-offline"></a>
### Stream offline

Message: "{{chat:govee.refund.offline}}"

Meaning: the viewer redeemed while the stream was offline and "{{console:govee.liveOnlyLabel}}" is on. It is on by default. The streamer can go live, or turn it off to allow offline changes.

<a id="chat-colour"></a>
### Colour not recognized

Message: "{{chat:govee.refund.colour}}"

Tell the viewer to type an English colour name like blue, or a hex code like #00ccff. If "{{console:govee.allowOffLabel}}" is on, off turns the light off.

<a id="chat-setup"></a>
### Lights not set up

Message: "{{chat:govee.refund.setup}}"

Meaning: the bot found no working Govee setup for this light. Tell the streamer to open the Govee Lights page and check that "{{console:govee.keyOnFile}}" shows and that the light has a reward. If the key was rejected, see [Govee no longer accepts the key](#key-rejected).

<a id="chat-busy"></a>
### Too many changes

Message: "{{chat:govee.refund.busy}}"

Meaning: colour changes came in too fast. Wait a moment and redeem again.

<a id="chat-unreachable"></a>
### Could not reach the lights

Message: "{{chat:govee.refund.unreachable}}"

Meaning: the bot could not reach Govee or the light. Tell the streamer to check that the lights are on and connected to Wi-Fi, then try again in a moment.
