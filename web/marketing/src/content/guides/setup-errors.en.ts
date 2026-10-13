// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  "slug": "setup-errors",
  "meta": {
    "title": "Setup errors - ItsBagelBot Guides",
    "description": "The setup and error messages ItsBagelBot shows for sign-in, Spotify, Govee, Discord, share access, commands, data sources, channel points and bot status, with what it means and what to do.",
    "eyebrow": "Guide",
    "heading": "Setup errors",
    "lead": "Paste the message you saw, find it in the list, and follow the fix. Each row links to the guide that covers the whole flow.",
    "minutes": "Reference",
    "card": {
      "title": "Setup errors",
      "description": "An error glossary: the exact message on screen or in chat, what it means, and what to do, grouped by Spotify, Govee, Discord, share access and more.",
      "meta": "200 messages · 8 areas",
      "chips": [
        "sign-in",
        "Spotify",
        "Govee",
        "Discord",
        "bot status"
      ]
    }
  },
  "sections": [
    {
      "id": "use",
      "heading": "How to use this page",
      "note": "Paste or find your message, then follow the row.",
      "blocks": [
        {
          "kind": "prose",
          "html": "\n            <p>\n                This page lists the messages ItsBagelBot shows on screen and in chat, grouped by area.\n                Find the message you were shown, read what it means, and follow the steps in the last\n                two columns. The last column links to the guide that explains the whole flow.\n                You can use this page without signing in. Messages are copied exactly as they\n                appear, so you can paste yours and match it. Words in curly braces, such as\n                <code>&#123;max&#125;</code>, or <code>@viewer</code>, stand for a number or a name that changes.\n            </p>"
        },
        {
          "kind": "callout",
          "tone": "tip",
          "html": "\n                <b>For an AI assistant</b>\n                Match the pasted text to the first column. Give the user the exact button names from the last column's guide. Never ask the user for a Client Secret, an API key, or a password."
        }
      ]
    },
    {
      "id": "signin",
      "heading": "Sign-in",
      "note": "Messages on the sign-in page, before you reach the dashboard.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "Sign-in was cancelled on Twitch. Choose Continue with Twitch and select Authorize to sign in.",
              "You cancelled on Twitch, so nothing was signed in.",
              "Select <strong>Continue with Twitch</strong>, then <strong>Authorize</strong>.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "That sign-in took too long or was started in another tab. Choose Continue with Twitch to try again.",
              "The sign-in sat too long, or you started it in another tab.",
              "Select <strong>Continue with Twitch</strong> again and finish in the same tab.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Twitch did not finish the sign-in. Wait a moment, then choose Continue with Twitch again.",
              "Twitch did not complete the sign-in.",
              "Wait a moment, then select <strong>Continue with Twitch</strong> again.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Twitch did not grant the sign-in permission. Choose Continue with Twitch and approve every permission on the screen.",
              "Twitch did not grant the sign-in permission.",
              "Select <strong>Continue with Twitch</strong> and approve every permission on the Twitch screen.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Sign-in did not finish on the server. Nothing was saved. Please try again.",
              "The sign-in did not finish on the ItsBagelBot side.",
              "Try again. Nothing was saved, so there is nothing to undo.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "That is the bot's Twitch account. Sign out of it on twitch.tv, then sign in here with your own channel.",
              "You are signed in to Twitch with the bot account, not your own channel.",
              "Sign out of that account on twitch.tv, then sign in here with your own channel.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "That share link is no longer valid. Ask the broadcaster for a new one.",
              "The share link was revoked, already used, or mistyped.",
              "Ask the broadcaster for a new link.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "You were signed out. That account no longer exists on ItsBagelBot.",
              "The account you were using was deleted.",
              "Sign in again with a Twitch account to start fresh.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "You were signed out on every device. Sign in again to continue.",
              "Your sessions were ended on every device.",
              "Sign in again with Twitch.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "This account can no longer use the console.",
              "The account is blocked from the console.",
              "Write to support through the Contact page.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "spotify",
      "heading": "Spotify",
      "note": "Messages from the Song Requests page and from chat when viewers request songs.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "Spotify Premium is required",
              "The connected account needs Premium. The account that owns your Spotify app needs Premium too, and each app can connect at most 5 accounts.",
              "Connect an account with Premium, make sure the app owner has Premium, and keep to 5 accounts.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "You closed the Spotify permission screen before approving. Choose Connect Spotify and approve the permissions to finish.",
              "You left Spotify's permission screen without approving, so nothing was connected.",
              "Select <strong>Connect Spotify</strong> again and approve the permissions on Spotify's screen.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "The sign-in link expired or was replayed. Start the connect again.",
              "The sign-in link you used is no longer valid.",
              "Select <strong>Connect Spotify</strong> again to start a fresh sign-in.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify refused the sign-in. Check that the Client ID and Client Secret match your Spotify app, and that the redirect URI shown on this page is saved in that app. Then connect again.",
              "Spotify rejected the credentials or the Redirect URI.",
              "Copy the Client ID and Client Secret again from your app's Settings. Check that the Redirect URI shown in the dashboard is saved in your Spotify app, character for character. Then connect again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify did not send a new sign-in token. Open spotify.com/account/apps, remove access for your app, then choose Connect Spotify and agree again.",
              "Spotify did not issue a new token for this connection.",
              "Open <code>spotify.com/account/apps</code>, remove access for your app, then select <strong>Connect Spotify</strong> and approve the permissions again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Add your Spotify app before connecting your account.",
              "No Spotify app is saved yet.",
              "Go back to the <strong>Add your keys</strong> stage, paste the Client ID and Client secret, and select <strong>Save &amp; continue</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify setup is unavailable on our side right now. Try again later. If it lasts more than a day, email support@itsbagelbot.com.",
              "The problem is on the ItsBagelBot side, not in your Spotify app.",
              "Try again later. If it lasts more than a day, email <code>support@itsbagelbot.com</code>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "The token could not be stored. Try again in a moment.",
              "Spotify accepted the connection, but ItsBagelBot could not save the token.",
              "Wait a moment and select <strong>Connect Spotify</strong> again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "That does not look like a Client ID and Client Secret. Copy both again from your app's Settings page in the Spotify Developer Dashboard, with no spaces.",
              "The two values did not look like a valid Client ID and Client Secret, so they were not saved.",
              "Copy both again from your app's Settings page in Spotify, paste them with no spaces, and select <strong>Save &amp; continue</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not save the Spotify app.",
              "The credentials could not be saved.",
              "Try <strong>Save &amp; continue</strong> again. If it keeps failing, check the values and try later.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "The Redirect URI is unavailable on our side right now, so you cannot continue yet. Try again later.",
              "The dashboard cannot show you the Redirect URI to copy.",
              "Try again later.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Your Spotify connection is missing a permission song requests needs. Reconnect to grant it.",
              "The Spotify connection was made without a permission the requests need.",
              "Select <strong>Reconnect Spotify</strong> and approve the permissions on Spotify's screen.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Your Twitch sign-in is missing the channel points permission this reward needs. Sign in with Twitch again to grant it.",
              "Your Twitch sign-in does not include the channel points permission. This is a Twitch permission, not a Spotify one.",
              "Select <strong>Sign in with Twitch again</strong> and approve the permission.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify no longer accepts this connection, so song requests are paused. Reconnect to resume them.",
              "Spotify stopped accepting the saved connection. The page also shows <strong>Reconnect needed</strong>.",
              "Reconnect Spotify. If it fails, follow the row about \"Spotify did not send a new sign-in token\".",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Skipping needs Spotify Premium.",
              "The <strong>Skip</strong> button needs Premium on the connected account.",
              "Use an account with Premium.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "No active Spotify device. Start playing something first.",
              "Spotify is not playing on any device.",
              "Open Spotify on a device, start a song, then try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify no longer accepts this connection. Reconnect it and try again.",
              "The connection stopped working while you pressed <strong>Skip</strong>.",
              "Reconnect Spotify, then skip again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not reach the backend. Try again in a moment.",
              "The page could not load the song request settings.",
              "Wait a moment and reload the page.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Give the reward a title.",
              "The <strong>Reward title</strong> field is empty.",
              "Type a title, then save the reward again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Enter a whole number of points, at least 1.",
              "The <strong>Point cost</strong> is not a whole number of at least 1.",
              "Enter a whole number, 1 or more.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Enter a cooldown between 0 seconds and 7 days.",
              "The <strong>Global cooldown</strong> is outside the allowed range.",
              "Enter a cooldown from 0 seconds (off) up to 7 days.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not save the reward.",
              "The reward could not be saved.",
              "Wait a moment and save it again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not delete the reward.",
              "The reward could not be deleted.",
              "Wait a moment and try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not save the command settings.",
              "The <strong>Chat command (!sr)</strong> settings could not be saved.",
              "Change the setting again, or reload the page and retry.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not save the toggle.",
              "The <strong>Enable song requests</strong> switch could not be saved.",
              "Reload the page, check the switch, and try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not save the limits.",
              "The <strong>Per-viewer limits</strong> could not be saved.",
              "Select <strong>Save limits</strong> again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not skip the track. Try again in a moment.",
              "The <strong>Skip</strong> button did not go through.",
              "Wait a moment and skip again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not disconnect Spotify.",
              "The disconnect did not go through.",
              "Wait a moment and try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not remove the Spotify app.",
              "The removal of the saved Spotify app did not go through.",
              "Wait a moment and try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Could not copy. Select the URL and copy it manually.",
              "Your browser blocked the copy button.",
              "Select the Redirect URI with the mouse and copy it by hand.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests are not set up yet.",
              "Setup is not finished on the channel.",
              "Finish the setup above.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests are paused until the streamer reconnects Spotify.",
              "Spotify stopped accepting the saved connection.",
              "Select <strong>Reconnect Spotify</strong> on the Song Requests page.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests need Spotify Premium on this channel.",
              "The connected account does not have Premium.",
              "Connect an account with Premium.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer Spotify is not playing on any device right now.",
              "Spotify has no active device.",
              "Open Spotify and start playing a song.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer Spotify is busy right now, try again in a moment.",
              "Spotify is temporarily busy.",
              "Wait a moment and request again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer that Spotify link isn't supported. Share a track or album link.",
              "The viewer shared a link that is not a track or an album.",
              "Share a track or album link, or a song name.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer the music lookup is down right now, try again in a bit.",
              "The song lookup is unavailable for now.",
              "Try again later.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests from chat are turned off right now.",
              "<strong>Allow !sr</strong> is off.",
              "Turn on <strong>Allow !sr</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests are limited to a smaller group right now.",
              "The viewer is not in the group chosen under <strong>Who may queue</strong>.",
              "Change <strong>Who may queue</strong> if you want them to request.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer song requests only work while the stream is live.",
              "<strong>Live only</strong> is on and the stream is offline.",
              "Go live, or turn off <strong>Live only</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer no track found for that. Try a Spotify link or \"artist - song\".",
              "Spotify found no match.",
              "Try a Spotify link or an \"artist - song\" pair.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer you are at your song limit (N in the queue at once). !sr remove frees a slot.",
              "The viewer reached their limit under <strong>Per-viewer limits</strong>.",
              "Wait for a song to play, or raise the limit.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer the song queue is full right now.",
              "The queue has reached its size limit.",
              "Wait for songs to play.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "song requests from channel points are turned off right now, your points were refunded.",
              "<strong>Accept reward redemptions</strong> is off.",
              "Turn on <strong>Accept reward redemptions</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "song requests only work while the stream is live, your points were refunded.",
              "<strong>Live only</strong> is on in the Channel points card and the stream is offline.",
              "Go live, or turn off <strong>Live only</strong>.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "type a song name or a Spotify link, your points were refunded.",
              "The viewer redeemed the reward without typing a song.",
              "Redeem again and type a song name or a Spotify link.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "you are at your song limit for now, your points were refunded.",
              "The viewer reached their limit.",
              "Wait for a song to play.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "the song queue is full, your points were refunded.",
              "The queue has reached its size limit.",
              "Wait for songs to play.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "that track could not be queued, your points were refunded.",
              "Spotify could not add that track.",
              "Redeem again with another song.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "@viewer you don't have a queued song to remove.",
              "The viewer used <code>!sr remove</code> without a song in the queue.",
              "Nothing to fix. The viewer can request a song first.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "There is no song at position #N.",
              "A moderator removed a position that is not in the queue.",
              "Check the <strong>Queue</strong> card for the right position and try again.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify connected, but it refused to share this account. In development mode, the account that owns your Spotify app needs Premium. Upgrade that account in Spotify, then connect again.",
              "The connection worked, but Spotify refused to share this account. In development mode, the account that owns your Spotify app needs Premium.",
              "In Spotify, upgrade the account that owns your app to Premium. Then connect again from the Song Requests page.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ],
            [
              "Spotify connected, but this account is not on your app's allowed users. In the Spotify Developer Dashboard, add this account to your app's users, then connect again.",
              "The connection worked, but this account is not on your app's list of allowed users.",
              "In the Spotify Developer Dashboard, open your app's settings and add this Spotify account to the app's users. The name of that tab may differ from what you expect, so look for the user management settings. Then connect again from the Song Requests page.",
              "<a href=\"/guides/spotify-song-requests#trouble\">Spotify song requests: If something goes wrong</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "govee",
      "heading": "Govee",
      "note": "Messages from the Govee setup and from chat when viewers redeem the reward.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "\"Govee did not accept this key. Check it and try again.\"",
              "Govee rejected the key. The key was not saved.",
              "Copy the key again from the Govee email without extra spaces. If you applied for a newer key since, use the newest one. Older keys stop working when you generate a new one.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Key saved, but Govee did not answer, so it is not checked yet. Your lights will work once Govee is back. If they do not, paste the key again.\"",
              "The key is saved, but Govee did not answer when the dashboard checked it.",
              "Continue. Try the reward in a while. If the lights do not change, paste the key again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Could not save the key.\"",
              "The dashboard could not store the key.",
              "Try again in a moment.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Govee no longer accepts this API key. Choose Remove key, then paste a new key from the Govee Home app.\"",
              "The key was saved earlier, but Govee now rejects it. One cause is a newer key on the same account, which turns older keys off.",
              "Select \"Remove key\", apply for a new key in the Govee Home app, and paste it.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Could not load your Govee lights. Check your API key and try again.\"",
              "The dashboard could not get your light list from Govee. The key may be wrong, or Govee did not answer.",
              "Select \"Try again\". If it keeps failing, select \"Remove key\" and paste the key again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"No Govee devices found on this account.\"",
              "Govee returned no devices for this key. The lights may not be added to this Govee account, or they may only use Bluetooth.",
              "Add your lights in the Govee Home app and connect them to Wi-Fi, then select \"Refresh lights\".",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"No colour-capable Govee lights found on this account.\"",
              "Govee returned devices, but none can change colour. Only models that support colour appear.",
              "Add a colour light to your Govee account in the Govee app, then select \"Refresh lights\".",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Could not reach the backend. Try again in a moment.\"",
              "The dashboard could not reach ItsBagelBot.",
              "Reload the page in a moment.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Reconnect to grant channel-points access.\"",
              "The bot needs channel-points access to create rewards on your Twitch channel.",
              "Select \"Reconnect\" and approve the access on Twitch.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Enter your Govee API key.",
              "The key field was empty when you saved.",
              "Paste the key from the Govee email, then select \"Save &amp; continue\" again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Enter a reward title.",
              "The <strong>Reward title</strong> field is empty.",
              "Type a title, then save the reward again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Enter a whole number of points, at least 1.",
              "The <strong>Cost (points)</strong> is not a whole number of at least 1.",
              "Enter a whole number, 1 or more.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Enter a cooldown between 0 seconds and 7 days.",
              "The <strong>Cooldown</strong> is outside the allowed range.",
              "Enter a cooldown from 0 seconds (off) up to 7 days.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Could not toggle Govee lights.",
              "The <strong>Enable Govee lights</strong> switch did not save.",
              "Reload the page, check the switch, and try again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Could not remove the key.",
              "The saved key could not be removed.",
              "Wait a moment and select \"Remove key\" again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"Could not save the reward.\"",
              "The reward was not created or updated. One possible cause is a channel that is not a Twitch Affiliate or Partner, since the dashboard says Twitch only lets those channels create rewards.",
              "Check the title and cost, then try again. If it keeps failing, check that your channel is a Twitch Affiliate or Partner.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "Could not delete the reward.",
              "The reward could not be deleted.",
              "Wait a moment and try again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"@viewer the lights only change while the stream is live, your points were refunded.\"",
              "A viewer redeemed the reward while you were offline and \"Live only\" is on.",
              "Nothing. Go live, or turn off \"Live only\" if you want offline changes.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"@viewer that colour wasn't recognized, your points were refunded. Try a name like blue or a hex code like #00ccff.\"",
              "The viewer typed something that is not a known colour name or hex code.",
              "Tell the viewer to type an English colour name like blue, or a hex code like #00ccff.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"@viewer the lights are not set up yet, your points were refunded.\"",
              "The bot found no working Govee setup for this light when the reward was redeemed.",
              "Open the Govee Lights page. Check that \"Key on file\" shows and that the light has a reward.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"@viewer the lights are changing too often, your points were refunded. Try again in a moment.\"",
              "Too many colour changes came in at once, so the bot held this one back.",
              "Ask the viewer to try again in a moment. Add a \"Cooldown\" to the reward to space changes out.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ],
            [
              "\"@viewer couldn't reach the lights, your points were refunded.\"",
              "Govee or the light did not answer.",
              "Check that the light is on and shows as online in the Govee Home app. Then try again.",
              "<a href=\"/guides/govee-lights#trouble\">Govee Lights: If something goes wrong</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "discord",
      "heading": "Discord",
      "note": "Messages from the Discord page.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "\"Premium only while in beta\"",
              "Your channel is not Premium. Discord is open to Premium channels only during the beta.",
              "Choose \"See plans\". Come back to \"Discord\" once Premium is active.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord is in beta and open to Premium channels only.\"",
              "Same limit, shown when a Discord action is refused.",
              "Make the channel Premium, then try again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Premium for this channel is managed by &#123;login&#125;. Ask them to upgrade on their Billing page.\"",
              "You are a mod with shared access, and the Billing section is not shared with you.",
              "Ask the channel owner to upgrade on their Billing page.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "The \"Add a server\" button is greyed out, or \"Discord is not configured on this deployment (missing client id or redirect).\"",
              "Discord connection is not set up on Bagel's side. Nothing in your account causes it.",
              "Contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"You cancelled on Discord's screen, so nothing was added. Choose Add a server to try again.\"",
              "You closed or cancelled Discord's confirm screen.",
              "Choose \"Add a server\" again and choose \"Authorize\" on Discord's screen.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord did not list a server you can add a bot to. You need to own the server, or have Manage Server on it.\"",
              "The Discord account you used has no server it can add Bagel to.",
              "Use the Discord account that owns the server or has Manage Server on it.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"No server we can add Bagel to\"",
              "The server list is empty: \"Bagel can only be added to a server you own, or one where you have Manage Server.\"",
              "Ask a server owner for Manage Server, or choose \"Create my Discord\" to start from Bagel's template.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Linked to another channel\"",
              "Another Twitch channel already uses this server.",
              "Ask that channel's owner to choose \"Disconnect\" on the server's Settings page in Bagel. Do not remove Bagel from the server.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"That Discord server is already linked to another Twitch channel. Ask that channel's owner to open this server in Bagel, go to Settings and choose Disconnect. Then add it here again.\"",
              "You authorized Bagel on a server that belongs to another channel's setup.",
              "Ask that owner to choose \"Disconnect\", then add the server again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord refused that because Bagel is missing a permission in this server. In Discord, open Server Settings, then Roles, and drag Bagel's role above the roles it manages. If you unchecked a permission when adding Bagel, choose Re-authorize on this server's Settings page and keep them all checked.\"",
              "Bagel's role sits too low, or a permission was unchecked.",
              "Follow \"Fix the role order\" above. If you unchecked a permission, choose \"Re-authorize\" and keep every box checked.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"This server was added before Bagel asked for the Change Nickname permission, so the premium name cannot be applied here. The premium avatar still works. Choose Re-authorize Bagel, then choose Add Bagel again next to this server.\"",
              "The server was added with an older permission list.",
              "Choose \"Re-authorize Bagel\", then \"Add Bagel again\" next to the server, and keep every permission checked.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Bagel needs to be added again to work in this server.\"",
              "Bagel is not in the server any more, or needs a fresh authorization.",
              "Choose \"Add Bagel again\" on the server list.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord did not return a server. Try adding Bagel again.\"",
              "Discord ended the connection without naming a server.",
              "Choose \"Add a server\" and start again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"That Discord link expired. Start from Add Bagel again.\"",
              "The page waited too long, or came from an old tab.",
              "Open \"Discord\" and choose \"Add a server\" again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not save the connected server. Try again.\"",
              "Bagel could not store the link to your server.",
              "Choose \"Add a server\" and try again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Someone else saved this server first. Reload and try again.\"",
              "Two people changed the same server at once.",
              "Choose \"Reload\", then make your change again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord is rate limiting us. Try again in a minute.\"",
              "Discord is slowing requests down.",
              "Wait a minute and try again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord did not answer. Try again in a moment.\"",
              "Discord did not reply in time.",
              "Wait a moment and try again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"This took too long. Some of it may be missing; reload to see the rest.\"",
              "An action ran past its time limit and may be half done.",
              "Reload the page and check the result.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"That channel or role is gone from the server. Pick another one.\"",
              "The channel or role was deleted in Discord.",
              "Pick another one, or choose \"Create missing channels and roles\".",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"No Discord server is connected yet.\"",
              "This channel has no linked server, for example after \"Disconnect\".",
              "Choose \"Add a server\".",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Some settings were not valid, so they were left as they were.\" or \"Saved, but these were left as they were: &#123;fields&#125;.\"",
              "Some values were rejected and kept their old value.",
              "Check the fields marked \"Not valid, so this kept the value it already had.\" and fix them.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Bagel cannot post here\" or \"Bagel can no longer post in the saved channel. Give it Send Messages there, or pick another channel.\"",
              "Bagel lacks Send Messages in that channel.",
              "Give Bagel \"Send Messages\" in that channel in Discord, or pick another channel.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Turn the ticket desk on and save before reposting the panel.\"",
              "You asked to repost the ticket panel while tickets are off.",
              "Turn the ticket desk on, save, then repost.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not list your server's channels and roles, so the pickers are turned off. Nothing was changed; try again in a moment.\"",
              "Discord did not return the server's channels and roles.",
              "Wait a moment and reload the page.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not reach the backend. Try again in a moment.\"",
              "The dashboard could not load your Discord data.",
              "Wait a moment and reload.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not disconnect Discord.\" or \"Could not set up this server.\"",
              "The action did not finish.",
              "Try again. If it keeps failing, contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "The status \"Offline\", or \"The gateway dropped and is reconnecting on its own. Nothing to do.\"",
              "Bagel lost its connection to Discord and reconnects by itself.",
              "Wait a few minutes. If the status says \"Reauthorize\", choose \"Re-authorize Bagel\", then \"Add Bagel again\" next to the server.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Something went wrong on our side. Try again, and tell support if it keeps happening.\"",
              "An unexpected error on Bagel's side.",
              "Try again, then contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com if it repeats.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not save Discord settings.\"",
              "The settings on the server's page did not save.",
              "Try again. If it keeps failing, reload the page and make the change again.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Could not repost the ticket panel.\"",
              "Discord did not accept the ticket panel repost.",
              "Try again in a moment. Check that Bagel can post in the ticket channel.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord rejected the bot token. It has to be replaced before the bot can come back.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do from the dashboard. Contact support through the Contact page (itsbagelbot.com/contact) or at support@itsbagelbot.com if it lasts.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"This bot now needs more than one shard. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord refused the gateway intents. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Discord has not approved the privileged intents this bot needs. Nothing you can do from here; we are on it.\"",
              "A problem on Bagel's side, shown in the server status.",
              "Nothing you can do. Wait; Bagel's team is working on it.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ],
            [
              "\"Bagel joined. This server already has a layout, so it was not rebuilt. Bagel adopted the channels it recognised; pick the rest below.\"",
              "Bagel joined and kept your existing channels.",
              "Pick the channels that are still empty on the \"Channels\" page.",
              "<a href=\"/guides/discord-setup#trouble\">Add Bagel to Discord: If something goes wrong</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "share",
      "heading": "Share access",
      "note": "Messages about share links, billing access, and Premium checkout or gifts.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "\"That share link is no longer valid. Ask the broadcaster for a new one.\" (sign-in page)",
              "The link was already used, was revoked, or is incomplete.",
              "Mod: ask the broadcaster for a new link. Owner: check \"Shared access\", then create a new link with \"New share link\".",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Sign-in did not finish on the server. Nothing was saved. Please try again.\" (sign-in page)",
              "The sign-in stopped before the link was accepted.",
              "Open the share link again. If it then shows the \"no longer valid\" message, ask for a new link.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"That dashboard is no longer shared with you. Ask the owner to send you a new share link.\" (Settings)",
              "The owner revoked your access, or it ended.",
              "Ask the owner for a new share link.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Could not open that dashboard right now. Wait a moment, then choose Open again.\" (Settings)",
              "Bagel could not check your access at that moment.",
              "Wait a moment, then choose \"Open\" again.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"That share link is yours, so it opened your own dashboard. It still works. Send it to your mod to open while signed in to their own Twitch account.\" (Settings)",
              "You opened your own link while signed in as the owner. Nothing was used up.",
              "Send the same link to the mod. They open it signed in to their own Twitch account.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Premium for this channel is managed by &#123;login&#125;. Ask them to upgrade on their Billing page.\"",
              "You are a mod, a Premium beta page needs Premium, and Billing is not shared with you. &#123;login&#125; is the owner.",
              "Ask &#123;login&#125; to upgrade on their Billing page. Owners can also tick \"Billing\" under \"Edit access\".",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"You do not have access to manage billing.\"",
              "Billing is not part of what was shared with you.",
              "Ask the owner to add \"Billing\" under \"Edit access\", or to handle it.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Only the account owner can do that.\"",
              "That action is reserved for the owner.",
              "Ask the owner to do it.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Pick at least one section.\"",
              "The form was sent with every section unticked.",
              "Tick at least one section, then choose \"Generate link\" or \"Save\".",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Clipboard blocked. Select the link manually.\"",
              "Your browser blocked \"Copy\".",
              "Select the link under \"Invite links\" and copy it yourself.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Could not create link.\" or \"Could not update link.\" or \"Could not revoke link.\"",
              "The action did not finish.",
              "Try again in a moment. Check \"Shared access\" to see what was saved.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "\"Could not leave dashboard.\"",
              "Leaving did not finish.",
              "Try \"Leave\" again in a moment.",
              "<a href=\"/guides/share-access#trouble\">Share your dashboard with a mod: If something goes wrong</a>"
            ],
            [
              "Checkout could not open right now. Nothing was charged. Wait a minute, then try again.",
              "Checkout did not open.",
              "Wait a minute, then try again.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "Subscription management could not open right now. Your plan has not changed. Try again later.",
              "Subscription management did not open.",
              "Try again later. Your plan is unchanged.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "That streamer has not signed in to ItsBagelBot yet. Ask them to sign in once, then send the gift again.",
              "The gift target never signed in to ItsBagelBot.",
              "Ask them to sign in once, then send the gift again.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "That account cannot receive Premium. Pick a different streamer.",
              "That account cannot receive Premium.",
              "Pick a different streamer.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "That streamer already has Premium. Pick someone else to gift to.",
              "The streamer already has Premium.",
              "Pick someone else.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "Could not check that username right now. Wait a moment, then try again.",
              "The username check failed for now.",
              "Wait a moment, then try again.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "That is your own account. To get Premium yourself, choose Subscribe monthly or Buy one month.",
              "You typed your own username in the gift field.",
              "Choose <strong>Subscribe monthly</strong> or <strong>Buy one month</strong> instead.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "A checkout for that account is already open. Wait 30 seconds, then try again.",
              "A checkout for that account is already open.",
              "Wait 30 seconds, then try again.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "This account already has Premium, or a prize month is on the way. Refresh this page to see your current plan.",
              "The account already has Premium, or a prize month is on the way.",
              "Refresh the page to see your current plan.",
              "<a href=\"/pricing\">Pricing</a>"
            ],
            [
              "Gift notes cannot contain links or web addresses. Please remove them and try again.",
              "The gift note contains a link.",
              "Remove the link or web address and try again.",
              "<a href=\"/pricing\">Pricing</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "commands",
      "heading": "Commands and data sources",
      "note": "Messages from the command editor and the data source editor.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "Response is required.",
              "The command reply is empty.",
              "Type a reply, then save.",
              "<a href=\"/guides/commands#create\">Commands & variables: Two ways to create one</a>"
            ],
            [
              "Reply is too long (max {max} characters).",
              "The reply is over the length limit. {max} stands for the limit.",
              "Shorten the reply or split it into lines.",
              "<a href=\"/guides/commands#rules\">Commands & variables: The rules of the road</a>"
            ],
            [
              "Cooldown must be a whole number of seconds.",
              "The cooldown has a decimal or a letter.",
              "Type a whole number of seconds.",
              "<a href=\"/guides/commands#rules\">Commands & variables: The rules of the road</a>"
            ],
            [
              "User restriction must be a numeric Twitch user id.",
              "The user restriction is a username, not an ID.",
              "Type the numeric Twitch user ID, not the username.",
              "<a href=\"/guides/commands#rules\">Commands & variables: The rules of the road</a>"
            ],
            [
              "Enter a name for this data source.",
              "The data source has no name.",
              "Type a name, then save.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "Enter the web address to fetch.",
              "The data source has no web address.",
              "Type the web address to fetch.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "Give the key a label first.",
              "The key has no label.",
              "Type a label, then save.",
              "<a href=\"/guides/data-sources#keys\">Data sources: APIs that need a key</a>"
            ],
            [
              "Paste the key value first.",
              "The key value is empty.",
              "Paste the key value, then save.",
              "<a href=\"/guides/data-sources#keys\">Data sources: APIs that need a key</a>"
            ],
            [
              "A key labeled \"{label}\" already exists. Use Rotate to replace it.",
              "A key already uses that label.",
              "Use <strong>Rotate</strong> on the existing key, or pick another label.",
              "<a href=\"/guides/data-sources#keys\">Data sources: APIs that need a key</a>"
            ],
            [
              "Could not save the key.",
              "The key could not be saved.",
              "Try again in a moment.",
              "<a href=\"/guides/data-sources#keys\">Data sources: APIs that need a key</a>"
            ],
            [
              "Use lower-case letters, digits and underscores only.",
              "The data source name has a capital letter, a space or a symbol.",
              "Use lower-case letters, digits and underscores only.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "Data source name must be at most 32 characters.",
              "The data source name is too long.",
              "Shorten the name.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "URL must be at most 512 characters.",
              "The web address is too long.",
              "Use a shorter address.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "URL must start with https://",
              "The web address does not start with https.",
              "Start the address with https://.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "URL must point at a public https host.",
              "The web address points to a private or local host.",
              "Use the public https address of the API.",
              "<a href=\"/guides/data-sources#create\">Data sources: Add one from the command editor</a>"
            ],
            [
              "Response can be at most 5 lines. Each line is sent as its own chat message.",
              "The command reply has more than 5 lines.",
              "Remove lines until there are 5 or fewer.",
              "<a href=\"/guides/commands#rules\">Commands & variables: The rules of the road</a>"
            ],
            [
              "Each line must be at most 500 characters.",
              "A line of the command reply is too long.",
              "Shorten the line or split it into two lines.",
              "<a href=\"/guides/commands#rules\">Commands & variables: The rules of the road</a>"
            ],
            [
              "The API refused the request. Check the URL, and pick the right key under API key if this API needs one.",
              "The API turned the test away: a wrong or missing key, or a wrong address.",
              "Check the web address. If the API needs a key, add it in Settings under API keys, pick it in the API key field, and test again.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "Rate limited. Wait a moment and test again.",
              "A request cap was reached.",
              "Wait a minute and test again.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "The API returned an error. Chat sees nothing, unless the token has a |default: &#123;urlfetch:name|default&#125;.",
              "The API answered with an error.",
              "Check the API status, or add a fallback after a pipe.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "The API timed out. Chat sees nothing, unless the token has a |default: &#123;urlfetch:name|default&#125;.",
              "The API took longer than the bot waits.",
              "Try again, and add a fallback after a pipe.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "This data source could not produce a value. Until it does, chat shows the token's |default value if it has one, otherwise &#123;urlfetch:name&#125; as typed. Check the web address and the API key, then click Fetch a sample again.",
              "The source is paused, or its path or address cannot give a value.",
              "Resume the source if it is paused. Otherwise fix the address or key, fetch a sample, and click the value again.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "The fetch service did not answer. Try again in a moment.",
              "The service that calls your API did not reply in time.",
              "Wait a moment and test again.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "The API answered, but its response could not be read automatically. Paste one below instead.",
              "The answer is not readable text, or it is over 128 KB.",
              "Click \"or paste a response instead\" and paste a real response by hand.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "That is not valid JSON. Fix it and the tree appears here.",
              "The pasted sample is not JSON.",
              "Paste the response again, from the first brace to the last.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "That sample is over 128 KB. Paste a shorter one.",
              "The pasted sample is too large for the tree.",
              "Paste a smaller slice of the response.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "Too many test runs. Each one calls the real API. Wait about 10 seconds and try again.",
              "Fetch a sample allows about one test every 10 seconds.",
              "Wait 10 seconds.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "You've reached the 20 data source limit for your channel. Delete one to make room.",
              "A channel holds 20 data sources.",
              "Delete a source you no longer use.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "A definition named \"name\" already exists.",
              "Another source already has that name.",
              "Pick a different name, or edit the existing source.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "A timer message can reference at most 3 different fetched values (&#123;urlfetch:…&#125;).",
              "Timer messages are capped at 3 data sources.",
              "Quote fewer sources in the timer message.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ],
            [
              "A raw &#123;urlfetch:name&#125; in chat",
              "The source is paused, missing, or cannot produce a value, and the token has no |default. Tokens past the per-message cap also stay raw.",
              "Open the command, check the name matches a saved source, and resume it if paused. Add a |default so chat shows a fallback instead.",
              "<a href=\"/guides/data-sources#errors\">Data sources: What chat shows when it fails</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "points",
      "heading": "Channel points",
      "note": "Messages when you create or edit a channel-points reward.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "Enter a name for this reward.",
              "The reward has no title.",
              "Type a title (up to 45 characters).",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Title is required (max 45 characters).",
              "The title is empty or too long.",
              "Use 45 characters or fewer.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "A reward with this title already exists on your channel. Pick another title.",
              "Twitch already has a reward with that title.",
              "Pick another title.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Twitch only lets Affiliate and Partner channels create channel point rewards. Once your channel is an Affiliate, click Create reward again.",
              "Twitch gives channel point rewards only to Affiliate and Partner channels.",
              "Become an Affiliate, then select <strong>Create reward</strong> again.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Enter a whole number of points, at least 1.",
              "The cost is not a whole number of at least 1.",
              "Type a whole number of points, at least 1.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Enter a valid point cost.",
              "The point cost is not valid.",
              "Type a valid cost.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Enter a whole number, at least 1.",
              "A limit field is not a whole number of at least 1.",
              "Type a whole number, at least 1.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Enter a cooldown between 1 second and 7 days.",
              "The cooldown is outside the range Twitch allows.",
              "Type a cooldown from 1 second to 7 days.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ],
            [
              "Could not save the reward.",
              "The reward was not saved.",
              "Try again. If it keeps failing, check the fields above.",
              "<a href=\"/guides/govee-lights#reward\">Govee Lights: Create the channel-points reward</a>"
            ]
          ]
        }
      ]
    },
    {
      "id": "status",
      "heading": "Bot status",
      "note": "Messages on the Bot status card and at the end of the setup.",
      "blocks": [
        {
          "kind": "table",
          "head": [
            "What you see",
            "What it means",
            "What to do",
            "Guide section"
          ],
          "rows": [
            [
              "Not connected: \"The bot has no Twitch authorization for your channel.\"",
              "The bot is not signed in to your channel.",
              "Select <strong>Connect</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Disabled by you: \"Your bot is switched off. Turn it back on to rejoin chat.\"",
              "You switched the bot off.",
              "Select <strong>Enable</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Reconnect needed or Twitch access lost: \"Twitch revoked the bot’s access to your channel. This happens after a password change or if the app was disconnected on Twitch. Reconnect your Twitch account to bring the bot back.\"",
              "Twitch no longer lets the bot act for your channel.",
              "Select <strong>Reconnect</strong>. It takes you to <strong>Settings</strong>. There, choose <strong>Reconnect Twitch</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Bot banned from your chat: \"ItsBagelBot is banned from your chat. To unban it, type /unban ItsBagelBot in your Twitch chat (you or one of your moderators can), or remove it from the banned list in your Twitch Creator Dashboard moderation settings. It rejoins on its own after that.\" or \"ItsBagelBot is banned from your chat. To unban it, type /unban ItsBagelBot in your Twitch chat (you or one of your moderators can), or remove it from the banned list in your Twitch Creator Dashboard moderation settings. Then select I unbanned it, check again.\"",
              "Your chat banned the bot, so it cannot join.",
              "Type <code>/unban ItsBagelBot</code> in your Twitch chat. It rejoins on its own. If the button says <strong>I unbanned it, check again</strong>, select it. If it says <strong>Enable and verify unban</strong>, select that.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "\"ItsBagelBot is still banned from your chat. Type /unban ItsBagelBot in your Twitch chat, then select Enable and verify unban.\"",
              "The check found the ban still in place.",
              "Unban the bot, then select <strong>Enable and verify unban</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Connection check failed: \"Access could not be confirmed. Retry when the problem is fixed.\"",
              "The bot could not confirm its access.",
              "Fix the problem, then select <strong>Retry connection check</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Dormant: \"The bot wakes automatically when an enabled feature is used. Your settings are preserved.\"",
              "The bot is resting until something needs it.",
              "Nothing. It wakes on its own.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Timed out: \"Normal bot work pauses until the timeout ends and access is verified.\"",
              "The bot is timed out in your chat.",
              "Wait for the timeout to end. The bot then checks its access again.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Connected · idle: \"The bot is connected but not replying in chat.\"",
              "The bot is in your chat but is not answering.",
              "Select <strong>Enable above</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "\"The bot lost its chat connection, so viewers might not get replies.\" or \"The bot lost its chat connection. Fix it in Settings → Reconnect\"",
              "The bot's chat connection dropped.",
              "Select <strong>Reconnect</strong>. It takes you to <strong>Settings</strong>. There, choose <strong>Reconnect Twitch</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Reconnecting…: \"Checking access before restoring operation.\"",
              "The bot is checking its access before it comes back.",
              "Wait a moment. The state changes on its own.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "\"Twitch authorization also needs reconnecting.\"",
              "A second Twitch permission needs renewing.",
              "Select <strong>Reconnect</strong> next to the message, then choose <strong>Reconnect Twitch</strong> in <strong>Settings</strong>.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Checking connection… or Connecting…",
              "The dashboard is still reading the bot's state.",
              "Wait a moment.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Connection status unavailable",
              "The dashboard could not read the bot's state.",
              "Reload the page in a moment.",
              "<a href=\"/guides/getting-started#bot-status\">Getting started: If the bot is not in chat</a>"
            ],
            [
              "Accept the Terms and Privacy Policy on the Legal agreements step, then press Finish setup.",
              "The setup was finished without accepting the legal agreements.",
              "Accept them on the step named in the message, then select <strong>Finish setup</strong>.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Pick a setup on the Tell us more step, then press Finish setup.",
              "No setup is chosen.",
              "Pick one on the step named in the message, then select <strong>Finish setup</strong>.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Your session ended. Sign in again to finish setup.",
              "Your session ended during setup.",
              "Sign in again, then finish setup.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Only the channel owner can finish setup. Sign in with the channel's own Twitch account.",
              "You are signed in with an account that is not the channel owner.",
              "Sign in with the channel’s own Twitch account.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ],
            [
              "Could not finish your setup. Some of your choices may already be saved. Press Finish setup to try again.",
              "The setup did not finish saving.",
              "Select <strong>Finish setup</strong> again.",
              "<a href=\"/guides/getting-started#connect\">Getting started: Connect your channel</a>"
            ]
          ]
        },
        {
          "kind": "callout",
          "tone": "note",
          "html": "Last checked: 12 October 2026."
        }
      ]
    }
  ]
};

export default guide;
