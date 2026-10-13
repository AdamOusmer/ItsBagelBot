// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'spotify-song-requests',
  meta: {
    title: 'Spotify song requests - ItsBagelBot Guides',
    description:
      'Set up Spotify song requests in ItsBagelBot: what you need (Premium, the 5-account limit), creating your Spotify app, pasting the Client ID and Client Secret, connecting your account, turning on !sr and the channel-points reward, and the messages you may see if something goes wrong.',
    eyebrow: 'Guide',
    heading: 'Spotify song requests',
    lead: 'Let viewers add songs to your Spotify queue from chat or with channel points. Create your own Spotify app, connect your account, and turn the requests on. About ten minutes.',
    minutes: '10 min read',
    card: {
      title: 'Spotify song requests',
      description:
        'Create your Spotify app, paste its keys, connect your account, then turn on !sr and the channel-points reward. Includes a table of the error messages and what to do.',
      meta: '10 min · 3 steps',
      chips: ['Spotify', '!sr', 'channel points', 'troubleshooting'],
    },
  },
  sections: [
    {
      id: 'need',
      heading: 'What you need',
      note: 'Spotify Premium, your own Spotify app, and Spotify playing on a device.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                You need an ItsBagelBot dashboard signed in with your Twitch account, and a Spotify
                account. The Spotify account you connect needs <strong>Premium</strong>, so the bot can
                control playback and add song requests to your queue. Song requests run on a Spotify
                app that you create yourself in Spotify's developer dashboard. The account that owns
                that app needs Premium too, and Spotify lets each app connect at most <strong>5
                accounts</strong>. Most streamers use one account for both jobs. Spotify must also be
                playing on a device (the desktop app, your phone, or a web player) while viewers
                request songs.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Development mode and other accounts</b>
                A new Spotify app starts in development mode. In that mode, an account that does not own
                the app has to be added to the app's list of allowed users in the Spotify developer
                dashboard, or Spotify refuses its requests. Connecting the account that owns the app
                avoids this.`,
        },
      ],
    },
    {
      id: 'setup',
      heading: 'Set up Spotify in the dashboard',
      note: 'One setup page, four stages: Welcome, Create your app, Add your keys, Connect Spotify.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Open <a href="https://dashboard.itsbagelbot.com/songqueue" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com/songqueue</a>.
                Until Spotify is connected, the page shows a setup with four stages: <strong>Welcome</strong>,
                <strong>Create your app</strong>, <strong>Add your keys</strong> and <strong>Connect Spotify</strong>.
                Follow the steps below in order. Each stage of the setup has an "Ask your AI for help"
                button, covered at the end of this guide.
            </p>`,
        },
        {
          kind: 'steps',
          items: [
            {
              title: 'Start the setup',
              html: `<p>On the <strong>Welcome</strong> stage, read the note "Spotify Premium is required", then select <strong>Let’s set up Spotify</strong>.</p>`,
            },
            {
              title: 'Open the Spotify developer dashboard',
              html: `<p>On the <strong>Create your app</strong> stage, select <strong>Open Spotify dashboard</strong>. It opens <code>developer.spotify.com/dashboard</code> in a new tab. Log in with the Spotify account that will own the app.</p>`,
            },
            {
              title: 'Create the app on Spotify',
              html: `<p>In Spotify's developer dashboard, select <strong>Create an App</strong>. Give the app a name and a description, accept the Developer Terms of Service, and create the app. Spotify shows the name and description to anyone who connects through it, so pick something you recognize.</p>`,
            },
            {
              title: 'Copy the Redirect URI',
              html: `<p>Go back to the ItsBagelBot tab. Under <strong>Redirect URI</strong>, select <strong>Copy</strong>. The button changes to "Redirect URI copied."</p>`,
            },
            {
              title: 'Paste it into your Spotify app',
              html: `<p>In Spotify, open your app's settings. Paste the value into the <strong>Redirect URIs</strong> field and save your changes. It must match exactly, without an extra slash or space. Spotify compares it character by character.</p>`,
            },
            {
              title: 'Confirm the app is created',
              html: `<p>Back in ItsBagelBot, select <strong>My app is ready</strong>. If the Redirect URI is missing, the button stays disabled and the page explains that the value is unavailable for now.</p>`,
            },
            {
              title: 'Copy your Client ID and Client Secret',
              html: `<p>On the <strong>Add your keys</strong> stage, switch to Spotify and open your app's <strong>Settings</strong>. Copy the <strong>Client ID</strong>. Reveal the <strong>Client Secret</strong> on the same page and copy it too.</p>`,
            },
            {
              title: 'Paste both in the dashboard',
              html: `<p>Paste the first value into <strong>Client ID</strong> and the second into <strong>Client secret</strong>, then select <strong>Save &amp; continue</strong>. Paste them with no spaces. ItsBagelBot stores the secret encrypted and never shows it again, so paste it again if you ever rotate it.</p>`,
            },
            {
              title: 'Connect your Spotify account',
              html: `<p>On the <strong>Connect Spotify</strong> stage, the page says "Your app is saved and ready." Select <strong>Connect Spotify</strong>. Spotify opens a permission screen. Approve the permissions on Spotify's screen. Closing the screen without approving stops the connection. A warning may appear right after you connect. The connection still works, and the table in "If something goes wrong" says what to do.</p>`,
            },
            {
              title: 'Check the result',
              html: `<p>Spotify sends you back to the Song Requests page. The Connection area shows the <strong>Connected</strong> badge and a "Spotify connected." message appears. You are now ready to turn the requests on.</p>`,
            },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Keep your Client Secret private</b>
                Paste the Client ID and Client Secret only into the ItsBagelBot dashboard fields shown
                above. Never paste a Client Secret into a chat with an AI, a Discord server or a
                support message. If it leaks, rotate it in the Spotify developer dashboard and paste
                the new one in ItsBagelBot.`,
        },
      ],
    },
    {
      id: 'requests',
      heading: 'Turn on song requests',
      note: 'Switch the module on, then choose chat, channel points, or both.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Once Spotify is connected, the Song Requests page shows the rest of the setup. Both
                request paths share one queue, and either can be on while the other is off.
            </p>`,
        },
        {
          kind: 'steps',
          items: [
            {
              title: 'Enable the module',
              html: `<p>Turn on <strong>Enable song requests</strong> at the top of the page. While it is off, the page says requests are paused and neither path queues anything.</p>`,
            },
            {
              title: 'Set up the chat command',
              html: `<p>In the <strong>Chat command (!sr)</strong> card, turn on <strong>Allow !sr</strong>. Under <strong>Who may queue</strong>, choose one of <strong>Only me</strong>, <strong>Moderators only</strong>, <strong>VIPs only</strong>, <strong>Subscribers only</strong> or <strong>Everyone</strong>. <strong>Live only</strong> is on by default, so requests work only while you are streaming. Turn it off to allow requests offline. These switches save as you change them.</p>`,
            },
            {
              title: 'Limit how many songs one viewer can queue',
              html: `<p>This one is optional. In <strong>Per-viewer limits</strong>, enter a number for each tier (<strong>Everyone</strong>, <strong>Subscribers</strong>, <strong>VIPs</strong>, <strong>Moderators</strong>) and select <strong>Save limits</strong>. A blank tier means no limit. You are never capped.</p>`,
            },
            {
              title: 'Create the channel-points reward',
              html: `<p>In the <strong>Channel points</strong> card, the row <strong>Song request</strong> says "Not set up" until you create the reward. Open it and select <strong>Create reward</strong>. Fill in <strong>Reward title</strong> (the default is "Play a song request"), <strong>Point cost</strong>, <strong>Global cooldown</strong> (0 = off) and <strong>Chat reply</strong>. <strong>Colour</strong> is optional. The reward appears in your Twitch points menu as soon as you save.</p>`,
            },
            {
              title: 'Choose what happens after a redemption',
              html: `<p>Under <strong>After redemption</strong>, pick one: "Fulfil: the song queues (default)", "Cancel: refund the points, skip the queue", or "Leave pending for mods to resolve manually".</p>`,
            },
            {
              title: 'Accept redemptions',
              html: `<p>Turn on <strong>Accept reward redemptions</strong>. While it is off, redemptions are refunded and nothing queues. <strong>Live only</strong> in this card is on by default too. Turn it off to allow redemptions offline.</p>`,
            },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Viewers type <code>!sr</code> followed by a song name, an "artist - song" pair, or a
                Spotify link. Channel-points redemptions work the same way: the viewer types the
                song when they redeem. The <strong>Queue</strong> card on the same page shows what is
                playing and what is waiting, and has a <strong>Skip</strong> button.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'A request from chat, as the bot answers it.',
          lines: [
            { who: 'viewer', name: 'SnackPackPanda', text: '!sr artist - song' },
            { who: 'bot', text: '@SnackPackPanda queued "Song" by Artist, position #2.' },
          ],
        },
      ],
    },
    {
      id: 'trouble',
      heading: 'If something goes wrong',
      note: 'The messages the setup and the module show, with what to do.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                The first table lists the messages you see in the dashboard. The second lists what
                viewers see in chat. Messages are copied as the dashboard shows them. The list covers the messages you are likely to see.
            </p>`,
        },
        {
          kind: 'table',
          id: 'dashboard-errors',
          caption: 'Messages in the dashboard.',
          head: ['What you see', 'What it means', 'What to do'],
          rows: [
            [
              'Spotify Premium is required',
              'The connected account needs Premium. The account that owns your Spotify app needs Premium too, and each app can connect at most 5 accounts.',
              'Connect an account with Premium, make sure the app owner has Premium, and keep to 5 accounts.',
            ],
            [
              "You closed the Spotify permission screen before approving. Choose Connect Spotify and approve the permissions to finish.",
              'You left Spotify\'s permission screen without approving, so nothing was connected.',
              'Select <strong>Connect Spotify</strong> again and approve the permissions on Spotify\'s screen.',
            ],
            [
              'The sign-in link expired or was replayed. Start the connect again.',
              'The sign-in link you used is no longer valid.',
              'Select <strong>Connect Spotify</strong> again to start a fresh sign-in.',
            ],
            [
              'Spotify refused the sign-in. Check that the Client ID and Client Secret match your Spotify app, and that the redirect URI shown on this page is saved in that app. Then connect again.',
              'Spotify rejected the credentials or the Redirect URI.',
              'Copy the Client ID and Client Secret again from your app\'s Settings. Check that the Redirect URI shown in the dashboard is saved in your Spotify app, character for character. Then connect again.',
            ],
            [
              'Spotify did not send a new sign-in token. Open spotify.com/account/apps, remove access for your app, then choose Connect Spotify and agree again.',
              'Spotify did not issue a new token for this connection.',
              'Open <code>spotify.com/account/apps</code>, remove access for your app, then select <strong>Connect Spotify</strong> and approve the permissions again.',
            ],
            [
              'Add your Spotify app before connecting your account.',
              'No Spotify app is saved yet.',
              'Go back to the <strong>Add your keys</strong> stage, paste the Client ID and Client secret, and select <strong>Save &amp; continue</strong>.',
            ],
            [
              'Spotify setup is unavailable on our side right now. Try again later. If it lasts more than a day, email support@itsbagelbot.com.',
              'The problem is on the ItsBagelBot side, not in your Spotify app.',
              'Try again later. If it lasts more than a day, email <code>support@itsbagelbot.com</code>.',
            ],
            [
              'The token could not be stored. Try again in a moment.',
              'Spotify accepted the connection, but ItsBagelBot could not save the token.',
              'Wait a moment and select <strong>Connect Spotify</strong> again.',
            ],
            [
              "That does not look like a Client ID and Client Secret. Copy both again from your app's Settings page in the Spotify Developer Dashboard, with no spaces.",
              'The two values did not look like a valid Client ID and Client Secret, so they were not saved.',
              'Copy both again from your app\'s Settings page in Spotify, paste them with no spaces, and select <strong>Save &amp; continue</strong>.',
            ],
            [
              'Could not save the Spotify app.',
              'The credentials could not be saved.',
              'Try <strong>Save &amp; continue</strong> again. If it keeps failing, check the values and try later.',
            ],
            [
              'The Redirect URI is unavailable on our side right now, so you cannot continue yet. Try again later.',
              'The dashboard cannot show you the Redirect URI to copy.',
              'Try again later.',
            ],
            [
              'Your Spotify connection is missing a permission song requests needs. Reconnect to grant it.',
              'The Spotify connection was made without a permission the requests need.',
              'Select <strong>Reconnect Spotify</strong> and approve the permissions on Spotify\'s screen.',
            ],
            [
              'Your Twitch sign-in is missing the channel points permission this reward needs. Sign in with Twitch again to grant it.',
              'Your Twitch sign-in does not include the channel points permission. This is a Twitch permission, not a Spotify one.',
              'Select <strong>Sign in with Twitch again</strong> and approve the permission.',
            ],
            [
              'Spotify no longer accepts this connection, so song requests are paused. Reconnect to resume them.',
              'Spotify stopped accepting the saved connection. The page also shows <strong>Reconnect needed</strong>.',
              'Reconnect Spotify. If it fails, follow the row about "Spotify did not send a new sign-in token".',
            ],
            [
              'Skipping needs Spotify Premium.',
              'The <strong>Skip</strong> button needs Premium on the connected account.',
              'Use an account with Premium.',
            ],
            [
              'No active Spotify device. Start playing something first.',
              'Spotify is not playing on any device.',
              'Open Spotify on a device, start a song, then try again.',
            ],
            [
              'Spotify no longer accepts this connection. Reconnect it and try again.',
              'The connection stopped working while you pressed <strong>Skip</strong>.',
              'Reconnect Spotify, then skip again.',
            ],
            [
              'Could not reach the backend. Try again in a moment.',
              'The page could not load the song request settings.',
              'Wait a moment and reload the page.',
            ],
            [
              "Give the reward a title.",
              "The <strong>Reward title</strong> field is empty.",
              "Type a title, then save the reward again.",
            ],
            [
              "Enter a whole number of points, at least 1.",
              "The <strong>Point cost</strong> is not a whole number of at least 1.",
              "Enter a whole number, 1 or more.",
            ],
            [
              "Enter a cooldown between 0 seconds and 7 days.",
              "The <strong>Global cooldown</strong> is outside the allowed range.",
              "Enter a cooldown from 0 seconds (off) up to 7 days.",
            ],
            [
              "Could not save the reward.",
              "The reward could not be saved.",
              "Wait a moment and save it again.",
            ],
            [
              "Could not delete the reward.",
              "The reward could not be deleted.",
              "Wait a moment and try again.",
            ],
            [
              "Could not save the command settings.",
              "The <strong>Chat command (!sr)</strong> settings could not be saved.",
              "Change the setting again, or reload the page and retry.",
            ],
            [
              "Could not save the toggle.",
              "The <strong>Enable song requests</strong> switch could not be saved.",
              "Reload the page, check the switch, and try again.",
            ],
            [
              "Could not save the limits.",
              "The <strong>Per-viewer limits</strong> could not be saved.",
              "Select <strong>Save limits</strong> again.",
            ],
            [
              "Could not skip the track. Try again in a moment.",
              "The <strong>Skip</strong> button did not go through.",
              "Wait a moment and skip again.",
            ],
            [
              "Could not disconnect Spotify.",
              "The disconnect did not go through.",
              "Wait a moment and try again.",
            ],
            [
              "Could not remove the Spotify app.",
              "The removal of the saved Spotify app did not go through.",
              "Wait a moment and try again.",
            ],
            [
              "Could not copy. Select the URL and copy it manually.",
              "Your browser blocked the copy button.",
              "Select the Redirect URI with the mouse and copy it by hand.",
            ],
            [
              "Spotify connected, but it refused to share this account. In development mode, the account that owns your Spotify app needs Premium. Upgrade that account in Spotify, then connect again.",
              "The connection worked, but Spotify refused to share this account. In development mode, the account that owns your Spotify app needs Premium.",
              "In Spotify, upgrade the account that owns your app to Premium. Then connect again from the Song Requests page.",
            ],
            [
              "Spotify connected, but this account is not on your app's allowed users. In the Spotify Developer Dashboard, add this account to your app's users, then connect again.",
              "The connection worked, but this account is not on your app's list of allowed users.",
              "In the Spotify Developer Dashboard, open your app's settings and add this Spotify account to the app's users. The name of that tab may differ from what you expect, so look for the user management settings. Then connect again from the Song Requests page.",
            ],
          ],
        },
        {
          kind: 'table',
          id: 'chat-errors',
          caption: 'What viewers see in chat. "@viewer" stands for the viewer\'s name and N for a number.',
          head: ['What you see', 'What it means', 'What to do'],
          rows: [
            [
              '@viewer song requests are not set up yet.',
              'Setup is not finished on the channel.',
              'Finish the setup above.',
            ],
            [
              '@viewer song requests are paused until the streamer reconnects Spotify.',
              'Spotify stopped accepting the saved connection.',
              'Select <strong>Reconnect Spotify</strong> on the Song Requests page.',
            ],
            [
              '@viewer song requests need Spotify Premium on this channel.',
              'The connected account does not have Premium.',
              'Connect an account with Premium.',
            ],
            [
              '@viewer Spotify is not playing on any device right now.',
              'Spotify has no active device.',
              'Open Spotify and start playing a song.',
            ],
            [
              '@viewer Spotify is busy right now, try again in a moment.',
              'Spotify is temporarily busy.',
              'Wait a moment and request again.',
            ],
            [
              "@viewer that Spotify link isn't supported. Share a track or album link.",
              'The viewer shared a link that is not a track or an album.',
              'Share a track or album link, or a song name.',
            ],
            [
              '@viewer the music lookup is down right now, try again in a bit.',
              'The song lookup is unavailable for now.',
              'Try again later.',
            ],
            [
              '@viewer song requests from chat are turned off right now.',
              '<strong>Allow !sr</strong> is off.',
              'Turn on <strong>Allow !sr</strong>.',
            ],
            [
              '@viewer song requests are limited to a smaller group right now.',
              'The viewer is not in the group chosen under <strong>Who may queue</strong>.',
              'Change <strong>Who may queue</strong> if you want them to request.',
            ],
            [
              '@viewer song requests only work while the stream is live.',
              '<strong>Live only</strong> is on and the stream is offline.',
              'Go live, or turn off <strong>Live only</strong>.',
            ],
            [
              '@viewer no track found for that. Try a Spotify link or "artist - song".',
              'Spotify found no match.',
              'Try a Spotify link or an "artist - song" pair.',
            ],
            [
              '@viewer you are at your song limit (N in the queue at once). !sr remove frees a slot.',
              'The viewer reached their limit under <strong>Per-viewer limits</strong>.',
              'Wait for a song to play, or raise the limit.',
            ],
            [
              '@viewer the song queue is full right now.',
              'The queue has reached its size limit.',
              'Wait for songs to play.',
            ],
            [
              'song requests from channel points are turned off right now, your points were refunded.',
              '<strong>Accept reward redemptions</strong> is off.',
              'Turn on <strong>Accept reward redemptions</strong>.',
            ],
            [
              'song requests only work while the stream is live, your points were refunded.',
              '<strong>Live only</strong> is on in the Channel points card and the stream is offline.',
              'Go live, or turn off <strong>Live only</strong>.',
            ],
            [
              'type a song name or a Spotify link, your points were refunded.',
              'The viewer redeemed the reward without typing a song.',
              'Redeem again and type a song name or a Spotify link.',
            ],
            [
              'you are at your song limit for now, your points were refunded.',
              'The viewer reached their limit.',
              'Wait for a song to play.',
            ],
            [
              'the song queue is full, your points were refunded.',
              'The queue has reached its size limit.',
              'Wait for songs to play.',
            ],
            [
              'that track could not be queued, your points were refunded.',
              'Spotify could not add that track.',
              'Redeem again with another song.',
            ],
            [
              "@viewer you don't have a queued song to remove.",
              "The viewer used <code>!sr remove</code> without a song in the queue.",
              "Nothing to fix. The viewer can request a song first.",
            ],
            [
              "There is no song at position #N.",
              "A moderator removed a position that is not in the queue.",
              "Check the <strong>Queue</strong> card for the right position and try again.",
            ],
          ],
        },
      ],
    },
    {
      id: 'ai',
      heading: 'Get help from an AI',
      note: 'The dashboard can copy a ready-made prompt that points to this guide.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Each stage of the setup has an <strong>Ask your AI for help</strong> button. It copies a
                prompt to your clipboard. The prompt names the stage you are on, includes the message
                the dashboard is showing, includes the Redirect URI, and contains a link to this guide.
                Paste it into ChatGPT, Claude or Gemini. Never paste your Client Secret into the chat.
                The AI does not need it.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Last checked: 12 October 2026.`,
        },
      ],
    },
  ],
};

export default guide;
