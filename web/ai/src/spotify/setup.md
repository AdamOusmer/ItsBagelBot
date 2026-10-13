---
title: Setup reference for AI assistants: {{console:setupAi.featureSpotify}}
description: Four dashboard steps to create a Spotify app, paste its Client ID and Client Secret, and connect a Spotify account. Read this first.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/guides/spotify-song-requests/
---

[Human guide](https://itsbagelbot.com/guides/spotify-song-requests/).

Create a Spotify app, save its credentials in the dashboard, and connect a Premium Spotify account.
Then enable chat requests, a channel-points reward, or both.

<page type="setup">

<audience>

The user is in the {{console:common.appName}} dashboard, on the "{{console:nav.songqueue}}" page, running the Spotify setup. The setup shows until Spotify is connected. It has four steps, numbered 01 to 04 in the step indicator at the top right: "{{console:spotifySetup.railWelcome}}", "{{console:spotifySetup.railApp}}", "{{console:spotifySetup.railKeys}}", "{{console:spotifySetup.railConnect}}". {{console:setupAi.guideStep}}

</audience>

<assistant_rules>

- Present one step at a time. Wait for the user to confirm or describe the result before continuing.
- {{console:setupAi.guideControls}}
- Do not request, accept, or output the Client Secret, tokens, or passwords. Direct the user to paste the Client Secret only into the dashboard field "{{console:spotify.app.clientSecretLabel}}". If a secret is pasted into chat, instruct the user to click the "ROTATE" button on the Spotify Developer Dashboard app overview and paste only the new secret into the dashboard.
- The Client ID is public and may be shared.
- Use only the exact Redirect URI the dashboard shows. Do not suggest `localhost` or an `http://` address.
- If an error appears, match it against [Spotify errors](https://ai.itsbagelbot.com/spotify/errors.md) and follow that section.
- Spotify changes its own screens. If a label differs, tell the user to look for the closest match and describe what they see.
- Follow only the buttons, fields, and limits documented on this page. If a setting is not covered, say so.
- {{console:setupAi.guideLanguage}}
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

## Before you start

- The Spotify account the user connects needs Premium. Without it Spotify refuses playback control and song requests do not queue.
- The Spotify account that owns the Spotify app needs Premium too. Most streamers use one account for both.
- A Spotify app in development mode connects at most 5 accounts. An account that does not own the app must be added to the app's allowed users in the Spotify Developer Dashboard, or Spotify refuses its requests. Connecting the owner account avoids this.
- Spotify must be playing on a device (desktop app, phone or web player) while viewers request songs.
- The dashboard note says: {{console:spotifySetup.premiumBody}}

</facts>

<a id="step-1"></a>
## Step 1: {{console:spotifySetup.railWelcome}}

Step name: "{{console:spotifySetup.railWelcome}}". Page title: "{{console:spotifySetup.welcomeTitle}}"

The page shows the note "{{console:spotifySetup.premiumTitle}}".

1. Ask the user to confirm the requirements in "Before you start".
2. Ask the user to select "{{console:spotifySetup.start}}".

How to tell it worked: the step indicator moves to the next number ("{{console:spotifySetup.railApp}}") and the title reads "{{console:spotifySetup.appTitle}}"

Common problems at this step:

- Premium missing: see [Premium required](errors.md#premium-required).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-2"></a>
## Step 2: {{console:spotifySetup.railApp}}

Step name: "{{console:spotifySetup.railApp}}". Page title: "{{console:spotifySetup.appTitle}}"

The page says: {{console:spotifySetup.appBody}}

In the dashboard:

1. Select "{{console:spotifySetup.developer}}". It opens https://developer.spotify.com/dashboard in a new tab.
2. Under "{{console:spotifySetup.redirectLabel}}" the page shows a value. Select "{{console:spotify.app.redirectCopy}}". The button then reads "{{console:spotify.app.redirectCopied}}"
3. Keep this tab open. Come back to it after the Spotify part.

In the Spotify Developer Dashboard (a separate site, a separate login):

1. Log in with the Spotify account that will own the app. It needs Premium.
2. Select "Create an App".
3. Enter an app name and a description. Spotify shows both to people who connect through the app.
4. If the form asks which API or SDK the app uses, tick "Web API". The form may differ by account. Tick it only if it appears.
5. Accept the Developer Terms of Service and create the app.
6. Open the app's settings ("Edit Settings" on the app page).
7. Paste the copied value into the Redirect URIs field. Add it, then select "SAVE".
8. Check the saved value against the dashboard value. It must match exactly: same letters, no extra slash, no space. Spotify requires https for redirect addresses and does not accept `localhost`. Use the value from the dashboard unchanged.

Back in the dashboard:

1. Select "{{console:spotifySetup.appCreated}}". The button is disabled while the Redirect URI is missing.

How to tell it worked: the step indicator moves to the next number ("{{console:spotifySetup.railKeys}}") and the title reads "{{console:spotifySetup.credentialsTitle}}"

Common problems at this step:

- No Redirect URI shown: see [Redirect URI unavailable](errors.md#redirect-missing).
- Copy button failed: see [Copy failed](errors.md#redirect-copy-failed).
- Spotify says the redirect URI is invalid or insecure: see [Spotify screen: invalid redirect URI](errors.md#spotify-invalid-redirect).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-3"></a>
## Step 3: {{console:spotifySetup.railKeys}}

Step name: "{{console:spotifySetup.railKeys}}". Page title: "{{console:spotifySetup.credentialsTitle}}"

The page says: {{console:spotifySetup.credentialsBody}}

In the Spotify Developer Dashboard:

1. Open the app the user created.
2. Open the app's "Settings".
3. Copy the Client ID.
4. Reveal the Client Secret and copy it. On current Spotify screens this is a "View client secret" link. The label may differ. If the user cannot find it, ask them to describe what they see on the Settings page.

In the dashboard:

1. Paste the first value into "{{console:spotify.app.clientIdLabel}}".
2. Paste the second value into "{{console:spotify.app.clientSecretLabel}}". The dashboard says: {{console:spotify.app.clientSecretHint}}
3. Paste with no spaces. Remind the user not to paste the secret into the AI chat.
4. Select "{{console:spotifySetup.saveContinue}}".

If the app is already saved, the step shows "{{console:spotifySetup.saved}}" and the buttons "{{console:spotify.app.replace}}", "{{console:spotify.app.remove}}" and "{{console:onboardingImport.continue}}". Use "{{console:spotify.app.replace}}" to enter new keys. Select "{{console:onboardingImport.continue}}" to move on.

How to tell it worked: the page shows "{{console:spotifySetup.saved}}" with the Client ID, and the step indicator moves to the next number ("{{console:spotifySetup.railConnect}}").

Common problems at this step:

- Keys rejected as malformed: see [Keys do not look right](errors.md#app-credentials).
- Save failed: see [Could not save the Spotify app](errors.md#app-save-failed).

Ask the user to confirm completion or describe what they see before continuing.

<a id="step-4"></a>
## Step 4: {{console:spotifySetup.railConnect}}

Step name: "{{console:spotifySetup.railConnect}}". Page title: "{{console:spotifySetup.connectTitle}}"

The page says: {{console:spotifySetup.connectBody}}

1. Select "{{console:spotify.connection.connectCta}}". Spotify opens a permission screen.
2. Log in to the Spotify account to connect. It needs Premium.
3. Approve the permissions on Spotify's screen. Closing the screen without approving stops the connection.
4. Spotify sends the user back to the dashboard.

If the connecting account is not the account that owns the Spotify app, it must first be added to the app's allowed users. In the Spotify Developer Dashboard, open the app's Settings and look for the user list (the tab may be called "User Management" or "Users and Access"). Add the account name and its Spotify email. Do this before selecting "{{console:spotify.connection.connectCta}}".

How to tell it worked: the page shows "{{console:spotifySetup.saved}}" and then the title "{{console:spotifySetup.readyTitle}}" with the button "{{console:spotifySetup.readyCta}}". On the "{{console:nav.songqueue}}" page the "{{console:spotify.connection.title}}" area shows "{{console:spotify.connection.connectedPill}}" and the message "{{console:spotify.connection.connectedToast}}"

Next: select "{{console:spotifySetup.readyCta}}". On the "{{console:nav.songqueue}}" page the user turns on "{{console:spotify.index.masterLabel}}", then chooses the chat command ("{{console:spotify.sr.enableLabel}}"), the channel-points reward ("{{console:spotify.redeem.enableLabel}}"), or both. "{{console:spotify.index.liveOnlyLabel}}" is on by default, so requests work only while the stream is live.

Common problems at this step:

- Closed the Spotify screen: see [Permission screen closed](errors.md#declined).
- Spotify refused the sign-in: see [Spotify refused the sign-in](errors.md#oauth).
- Sign-in link expired: see [Sign-in link expired](errors.md#state).
- No new token: see [No new sign-in token](errors.md#no-token).
- No app saved yet: see [Add your Spotify app first](errors.md#no-app).
- Token not stored: see [Token not stored](errors.md#store).
- Warning that the app owner needs Premium: see [Warning after connecting: app owner needs Premium](errors.md#connect-owner-premium).
- Warning that the account is not on the allowed users: see [Warning after connecting: account not on the allowed users](errors.md#connect-not-registered).
- Setup unavailable: see [Setup unavailable](errors.md#unconfigured).
- Spotify says the user is not registered, or the account lacks Premium: see [Spotify screen: user not registered](errors.md#spotify-user-not-registered) and [Premium required](errors.md#premium-required).


Ask the user to confirm completion or describe what they see before continuing.

<related>

- [Feature reference](../features/songqueue.md).
- [Setup](setup.md).
- [Errors](errors.md).
- [Human guide](https://itsbagelbot.com/guides/spotify-song-requests/).
- [Support](https://itsbagelbot.com/contact/) and [community](https://discord.gg/SZ2remwSDv).

</related>

</page>
