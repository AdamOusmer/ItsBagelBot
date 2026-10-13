---
title: Error reference for AI assistants: {{console:setupAi.featureSpotify}} errors
description: Every message the Spotify setup, the {{console:nav.songqueue}} page and chat can show, what it means, and what the user should do.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/spotify-song-requests/
---

[Human guide](https://itsbagelbot.com/guides/spotify-song-requests/).

Match the exact dashboard or chat message to an error below.
Connection, Spotify Premium, active-device, request-permission, and queue-limit failures need different fixes.

<page type="errors">

<audience>

Find the section whose message matches what the user sees. {{console:setupAi.guideLanguage}} In chat messages, `@name` stands for the viewer's name and `N` or `{...}` for a value.

</audience>

<assistant_rules>

- Explain matching error causes directly. If guiding active remediation, present one step at a time and wait for user confirmation before continuing.
- Do not request, accept, or output Client Secrets, tokens, or passwords. Direct the user to enter them only in the dashboard.
- {{console:setupAi.guideControls}} Setup steps are at [Spotify setup](https://ai.itsbagelbot.com/spotify/setup.md).
- {{console:setupAi.guideLanguage}}
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

The setup shows until Spotify is connected. After that the "{{console:nav.songqueue}}" page shows the module.

</facts>

## Setup and connection messages in the dashboard

<a id="generic-error"></a>
### {{console:setupAi.genericError}}

Message: "{{console:setupAi.genericError}}"

{{console:setupAi.guideGenericError}}

<a id="premium-required"></a>
### Premium required

Message: "{{console:spotifySetup.premiumTitle}}". Also shown in chat as "{{chat:songqueue.err.premium}}"

Meaning: the connected Spotify account does not have Premium. Spotify only allows playback control for Premium accounts. The account that owns the Spotify app needs Premium too, and each app connects at most 5 accounts.

Tell the user:

1. Check which account is connected and whether it has Premium.
2. Connect an account with Premium, or upgrade the account.
3. Check that the account that owns the Spotify app has Premium. If the owner lost Premium, the app stops working until it is active again.
4. Count the accounts connected to the app. The limit is 5.

<a id="declined"></a>
### Permission screen closed

Message: "{{console:spotify.errors.declined}}"

Meaning: the user left Spotify's permission screen without approving. Nothing was connected.

Tell the user:

1. Select "{{console:spotify.connection.connectCta}}" again.
2. Approve the permissions on Spotify's screen.

<a id="state"></a>
### Sign-in link expired

Message: "{{console:spotify.errors.state}}"

Meaning: the sign-in link is no longer valid.

Tell the user to select "{{console:spotify.connection.connectCta}}" again to start a new sign-in.

<a id="oauth"></a>
### Spotify refused the sign-in

Message: "{{console:spotify.errors.oauth}}"

Meaning: Spotify rejected the Client ID, the Client Secret or the Redirect URI.

Tell the user, one at a time:

1. In the Spotify Developer Dashboard, open the app and its Settings. Compare the Client ID with the one in the dashboard.
2. If the secret may be wrong, go back to "{{console:spotifySetup.railKeys}}", select "{{console:spotify.app.replace}}", and paste both values again from Spotify, with no spaces.
3. Compare the "{{console:spotifySetup.redirectLabel}}" in the dashboard with the one saved in the Spotify app, character by character. No extra slash, no space.
4. Select "{{console:spotify.connection.connectCta}}" again.

<a id="no-token"></a>
### No new sign-in token

Message: "{{console:spotify.errors.noToken}}"

Meaning: Spotify did not issue a new token because the account already approved this app.

Tell the user:

1. Open spotify.com/account/apps and remove access for the app.
2. Return to the dashboard and select "{{console:spotify.connection.connectCta}}".
3. Approve the permissions again.

<a id="no-app"></a>
### Add your Spotify app first

Message: "{{console:spotify.errors.noApp}}". Also: "{{console:spotify.connection.connectNeedsApp}}"

Meaning: no Spotify app keys are saved yet.

Tell the user to go to "{{console:spotifySetup.railKeys}}", paste the Client ID and Client secret, and select "{{console:spotifySetup.saveContinue}}".

<a id="unconfigured"></a>
### Setup unavailable

Message: "{{console:spotify.errors.unconfigured}}"

Meaning: the problem is on the {{console:common.appName}} side, not in the user's Spotify app.

Tell the user to try again later and, if it lasts more than a day, to email the address in the message.

<a id="store"></a>
### Token not stored

Message: "{{console:spotify.errors.store}}"

Meaning: Spotify accepted the connection, but the token could not be saved.

Tell the user to wait a moment and select "{{console:spotify.connection.connectCta}}" again.

<a id="connect-owner-premium"></a>
### Warning after connecting: app owner needs Premium

Message: "{{console:spotify.errors.connectOwnerPremium}}"

Meaning: the connection worked, but Spotify refused to share this account. In development mode, the account that owns the Spotify app needs Premium. This is a warning, not a failed connection.

Tell the user:

1. Find out which Spotify account owns the app. It is the account used to create the app in the Spotify Developer Dashboard.
2. In Spotify, upgrade that account to Premium. Spotify's own screens decide the exact steps, so ask the user to follow what Spotify shows.
3. Go back to the "{{console:nav.songqueue}}" page and connect again.

<a id="connect-not-registered"></a>
### Warning after connecting: account not on the allowed users

Message: "{{console:spotify.errors.connectNotRegistered}}"

Meaning: the connection worked, but this Spotify account is not on the app's list of allowed users. This is a warning, not a failed connection.

Tell the user:

1. Open the Spotify Developer Dashboard and select the app.
2. Open the app's Settings and look for the user list. The tab may be called "User Management" or "Users and Access". Spotify may use a different name.
3. Add the account that is connecting: its name and its Spotify email.
4. Go back to the "{{console:nav.songqueue}}" page and connect again.

<a id="app-credentials"></a>
### Keys do not look right

Message: "{{console:serverErrors.appCredentials}}"

Meaning: the Client ID or Client secret has the wrong shape, so nothing was saved. A common cause is an extra space or a partial copy.

Tell the user:

1. Copy both values again from the app's Settings page in the Spotify Developer Dashboard.
2. Paste them with no spaces into "{{console:spotify.app.clientIdLabel}}" and "{{console:spotify.app.clientSecretLabel}}".
3. Select "{{console:spotifySetup.saveContinue}}".

<a id="app-save-failed"></a>
### Could not save the Spotify app

Message: "{{console:spotify.app.saveFailed}}"

Meaning: the keys could not be saved.

Tell the user to select "{{console:spotifySetup.saveContinue}}" again. If it keeps failing, check both values and try later.

<a id="app-remove-failed"></a>
### Could not remove the Spotify app

Message: "{{console:spotify.app.removeFailed}}"

Tell the user to wait a moment and try again. Removing the app also disconnects the Spotify account.

<a id="redirect-missing"></a>
### Redirect URI unavailable

Message: "{{console:spotifySetup.redirectMissing}}"

Meaning: the dashboard cannot show the Redirect URI. "{{console:spotifySetup.appCreated}}" stays disabled.

Tell the user to wait and try again later. Do not give them a Redirect URI by hand.

<a id="redirect-copy-failed"></a>
### Copy failed

Message: "{{console:spotify.app.redirectCopyFailed}}"

Meaning: the browser blocked the copy button.

Tell the user to select the Redirect URI with the mouse and copy it by hand.

<a id="disconnect-failed"></a>
### Could not disconnect Spotify

Message: "{{console:spotify.connection.disconnectFailed}}"

Tell the user to wait a moment and try again.

## Messages on the "{{console:nav.songqueue}}" page

<a id="scope-gap"></a>
### Missing Spotify permission

Message: "{{console:spotify.connection.scopeGap}}"

Meaning: the connection was made without a permission song requests need.

Tell the user to select "{{console:spotify.connection.reconnectSpotify}}" and approve the permissions on Spotify's screen.

<a id="twitch-scope-gap"></a>
### Missing Twitch permission

Message: "{{console:spotify.connection.twitchScopeGap}}"

Meaning: this is a Twitch permission, not a Spotify one.

Tell the user to select "{{console:spotify.connection.reconnectTwitchCta}}" and approve the permission on Twitch.

<a id="grant-revoked"></a>
### Spotify no longer accepts the connection

Message: "{{console:spotify.connection.grantRevoked}}". The page also shows "{{console:spotify.connection.needsReconnectPill}}". In chat: "{{chat:songqueue.err.reconnect}}"

Meaning: Spotify stopped accepting the saved connection. Causes include removing the app's access on spotify.com/account/apps or changing the app's keys.

Tell the user:

1. Select "{{console:spotify.connection.reconnectSpotify}}".
2. Approve the permissions.
3. If it fails, follow [No new sign-in token](#no-token).

<a id="skip-premium"></a>
### Skip needs Premium

Message: "{{console:spotify.queue.skipPremium}}"

Tell the user the connected account needs Premium. See [Premium required](#premium-required).

<a id="skip-no-device"></a>
### No active device

Message: "{{console:spotify.queue.skipNoDevice}}". In chat: "{{chat:songqueue.err.no_device}}"

Meaning: Spotify is not playing on any device.

Tell the user to open Spotify on a device (desktop app, phone or web player), start a song, then try again.

<a id="skip-reauth"></a>
### Skip: connection no longer accepted

Message: "{{console:spotify.queue.skipReauth}}"

Tell the user to reconnect Spotify, then skip again. See [Spotify no longer accepts the connection](#grant-revoked).

<a id="skip-failed"></a>
### Could not skip

Message: "{{console:spotify.queue.skipFailed}}"

Tell the user to wait a moment and select "{{console:spotify.queue.skip}}" again.

<a id="degraded"></a>
### Backend unreachable

Message: "{{console:spotify.index.degraded}}"

Meaning: the page could not load the song request settings.

Tell the user to wait a moment and reload the page.

<a id="save-failed"></a>
### Could not save a setting

Messages:

- "{{console:spotify.index.masterFail}}" (the "{{console:spotify.index.masterLabel}}" switch)
- "{{console:spotify.sr.saveFailed}}" (the "{{console:spotify.sr.title}}" card)
- "{{console:spotify.quota.saveFailed}}" (the "{{console:spotify.quota.title}}" card)
- "{{console:spotify.toast.saveFailed}}" and "{{console:spotify.toast.deleteFailed}}" (the channel-points reward)

Tell the user to reload the page, check the setting, and try again.

<a id="reward-fields"></a>
### Reward field errors

Messages:

- "{{console:spotify.errors.titleRequired}}": the reward title is empty. Type a title and save again.
- "{{console:spotify.errors.cost}}": enter a whole number of points, 1 or more.
- "{{console:spotify.errors.cooldown}}": enter a cooldown from 0 seconds (off) up to 7 days.

## Messages on Spotify's own screens

These are Spotify's messages, not the dashboard's. The wording may change.

<a id="spotify-invalid-redirect"></a>
### Spotify screen: invalid redirect URI

Message: `INVALID_CLIENT: Invalid redirect URI`

Meaning: the Redirect URI in the Spotify app is not an exact match of the one the dashboard sends. Scheme, host, port, path and a trailing slash all count.

Tell the user:

1. Copy the value again from "{{console:spotifySetup.redirectLabel}}" in the dashboard.
2. In the Spotify app's settings, delete the old Redirect URI entry, paste the new one, and select "SAVE".
3. Select "{{console:spotify.connection.connectCta}}" again.

Related message: `INVALID_CLIENT: Insecure redirect URI`. Spotify only accepts https for these addresses, and does not accept `localhost`. The user must use the value from the dashboard unchanged.

<a id="spotify-user-not-registered"></a>
### Spotify screen: user not registered

Message: `User not registered in the Developer Dashboard` (Spotify answers with HTTP 403)

Meaning: the Spotify app is in development mode and the connecting account is not the owner or on the app's allowed user list. An app in development mode allows at most 5 users.

Tell the user:

1. In the Spotify Developer Dashboard, open the app's Settings and find the user list. The tab may be called "User Management" or "Users and Access".
2. Add the account's name and Spotify email. Stay within 5 users.
3. Select "{{console:spotify.connection.connectCta}}" again, or connect the account that owns the app.

## Messages viewers see in chat

<a id="chat-setup"></a>
### Not set up

Message: "{{chat:songqueue.err.setup}}"

Meaning: setup is not finished. Send the streamer to [setup](setup.md).

<a id="chat-busy"></a>
### Spotify busy

Message: "{{chat:songqueue.err.busy}}"

Meaning: Spotify is temporarily busy. Tell the viewer to try again in a moment.

<a id="chat-link"></a>
### Link not supported

Message: "{{chat:songqueue.err.link}}"

Tell the viewer to share a track or album link, or a song name.

<a id="chat-upstream"></a>
### Lookup down

Message: "{{chat:songqueue.err.upstream}}"

Meaning: the song lookup is unavailable. Try again later.

<a id="chat-sr-off"></a>
### Chat requests off

Message: "{{chat:songqueue.sr.off}}"

Tell the streamer to turn on "{{console:spotify.sr.enableLabel}}" on the "{{console:nav.songqueue}}" page. The module switch "{{console:spotify.index.masterLabel}}" must be on too.

<a id="chat-perm"></a>
### Smaller group

Message: "{{chat:songqueue.sr.perm}}"

Meaning: the viewer is not in the group chosen under "{{console:spotify.sr.permLabel}}". Options: "{{console:spotify.perm.broadcaster}}", "{{console:spotify.perm.mod}}", "{{console:spotify.perm.vip}}", "{{console:spotify.perm.sub}}", "{{console:spotify.perm.everyone}}". Tell the streamer to change it if they want the viewer to request.

<a id="chat-offline"></a>
### Stream offline

Message: "{{chat:songqueue.sr.offline}}"

Meaning: "{{console:spotify.index.liveOnlyLabel}}" is on, and it is on by default. Tell the streamer to go live or turn it off.

<a id="chat-no-track"></a>
### No track found

Message: "{{chat:songqueue.search.none}}"

Tell the viewer to try a Spotify link or an "artist - song" pair.

<a id="chat-quota"></a>
### Viewer at song limit

Message: "{{chat:songqueue.add.quota}}"

Meaning: the viewer reached the limit under "{{console:spotify.quota.title}}". Wait for a song to play, or the streamer raises the limit.

<a id="chat-full"></a>
### Queue full

Message: "{{chat:songqueue.add.full}}"

Meaning: the queue reached its size limit. Wait for songs to play.

<a id="chat-retract-none"></a>
### Nothing to remove

Message: "{{chat:songqueue.retract.none}}"

Meaning: the viewer has no queued song. Nothing to fix.

<a id="chat-remove-not-found"></a>
### No song at that position

Message: "{{chat:songqueue.remove.not_found}}"

Tell the streamer to check the "{{console:spotify.queue.title}}" card for the right position.

<a id="chat-refunds"></a>
### Channel-points refunds

Each message below means the bot refunded the viewer's points. In chat the line starts with the viewer's name and ends with the refund notice.

- "{{chat:songqueue.refund.off}}": "{{console:spotify.redeem.enableLabel}}" is off. Turn it on.
- "{{chat:songqueue.refund.offline}}": "{{console:spotify.index.liveOnlyLabel}}" is on and the stream is offline. Go live or turn it off.
- "{{chat:songqueue.refund.empty}}": the viewer typed no song. Redeem again with a song name or link.
- "{{chat:songqueue.refund.quota}}": the viewer is at their limit. Wait for a song to play.
- "{{chat:songqueue.refund.full}}": the queue is full. Wait.
- "{{chat:songqueue.refund.failed}}": Spotify could not add that track. Redeem again with another song.

<related>

- [Feature reference](../features/songqueue.md).
- [Setup](setup.md).
- [Errors](errors.md).
- [Human guide](https://itsbagelbot.com/guides/spotify-song-requests/).
- [Support](https://itsbagelbot.com/contact/) and [community](https://discord.gg/SZ2remwSDv).

</related>

</page>
