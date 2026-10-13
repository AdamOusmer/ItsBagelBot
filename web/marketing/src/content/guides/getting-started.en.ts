// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'getting-started',
  meta: {
    title: 'Getting started - ItsBagelBot Guides',
    description:
      'Set up ItsBagelBot in minutes: connect your Twitch channel, tour the dashboard, create your first command, and switch on your first module.',
    eyebrow: 'Guide',
    heading: 'Getting started',
    lead: 'From “Add to Twitch” to your first stream with the bot in chat. About seven minutes, most of it reading.',
    minutes: '7 min read',
    card: {
      title: 'Getting started',
      description:
        'From "Add to Twitch" to your first live stream with the bot: connect the channel, find your way around the dashboard, and switch on the first tools.',
      meta: '7 min · 6 sections',
      chips: ['connect', 'dashboard tour', 'first module'],
    },
  },
  sections: [
    {
      id: 'connect',
      heading: 'Connect your channel',
      note: 'One Twitch sign-in. No API keys, no config files, nothing to install.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Head to <a href="https://dashboard.itsbagelbot.com/auth/login" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a>
                and sign in with your Twitch account. Twitch shows you exactly what the bot is allowed
                to do before you approve anything, and that's the whole setup. The moment you're in,
                ItsBagelBot joins your chat and sits quietly until it's spoken to.
            </p>`,
        },
        {
          kind: 'prose',
          html: `
            <p>
                First time in, a short walkthrough greets you. Press <strong>Get set up</strong> and it
                runs in this order: <strong>Legal agreements</strong> (tick the box to accept the Terms
                and Privacy Policy), <strong>Make ItsBagelBot a mod</strong> (type
                <code>/mod ItsBagelBot</code> in your own chat), <strong>Choose your language</strong>
                for the console, then <strong>Tell us more</strong>, where you pick a setup:
                <strong>Start as a new streamer</strong>, <strong>Integrate with my setup</strong>,
                <strong>Completely quiet</strong> or <strong>Import setup</strong>. The new-streamer
                path adds two short stops, <strong>Commands are chat shortcuts</strong> and
                <strong>Modules add the extras</strong>, and ends with <strong>Finish setup</strong>.
                You can change every choice later in your dashboard.
            </p>
            <p>
                The mod step matters more than it looks: without mod status, Twitch silences the bot
                the moment your chat goes follower-only, sub-only or slow, and it just looks broken.
                You can also do it later.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'What your chat sees, about two seconds after you sign in.',
          lines: [
            { who: 'system', text: 'itsbagelbot joined #your_channel' },
            { who: 'viewer', name: 'SnackPackPanda', text: 'oh a new bot, hi!' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Good to know</b>
                The bot never speaks unprompted. Until you create commands or enable modules, joining
                is the only thing it does: your chat stays exactly as it was.`,
        },
      ],
    },
    {
      id: 'tour',
      heading: 'Find your way around',
      note: 'Six stops in the navigation rail. You will spend most of your time in two of them.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                The dashboard is one page at a time, and its whole navigation is the six-item rail
                down the left of the screen, which becomes a floating dock at the bottom on a phone.
                <strong>Overview</strong> is your landing page, <strong>Commands</strong> is where custom chat commands live, and
                <strong>Modules</strong> holds every bigger feature. <strong>Discord</strong> is its own
                page for connecting a Discord server (premium beta). Billing and Settings do what they
                say on the tin.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'DashboardHome',
          path: '/',
          caption: 'The Overview page. Navigation lives in the rail on the left, and in a dock at the bottom on a phone.',
          notes: [
            { n: 1, text: 'The rail is the whole navigation: Overview, Commands, Modules, Discord, Billing, Settings. On a phone the same six sit in a dock at the bottom.' },
            { n: 2, text: 'Bot status: whether ItsBagelBot is sitting in your chat right now, and the one button to fix it if not.' },
            { n: 3, text: 'Quick actions: the two things you will do most, one tap away.' },
            { n: 4, text: 'Your most-used commands live here too, one click from editing.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Everything on the dashboard saves the moment you confirm it: there's no "deploy" step,
                and changes usually reach your chat within seconds.
            </p>`,
        },
      ],
    },
    {
      id: 'first-command',
      heading: 'Create your first command',
      note: 'A name and a response. Everything else is optional.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Open <strong>Commands</strong> and press <strong>New command</strong>. A command needs
                exactly two things: a <strong>name</strong> (what viewers type after the
                <code>!</code>) and a <strong>response</strong> (what the bot says back). Let's make
                the classic:
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'CommandsList',
          path: '/commands',
          caption: 'The Commands page: your list on the left, the editor docked on the right.',
          notes: [
            { n: 1, text: 'New command opens the editor, docked beside the list on desktop (a bottom sheet on phones).' },
            { n: 2, text: 'The name, without the “!” (the dashboard adds it in chat). Lowercase, no spaces.' },
            { n: 3, text: 'The response. Plain text is fine; the next guide shows the variables that make it smart.' },
            { n: 4, text: 'Create saves it instantly, and the command goes live in chat right after.' },
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Thirty seconds later, in chat.',
          lines: [
            { who: 'viewer', name: 'SnackPackPanda', text: '!discord' },
            { who: 'bot', text: 'Come hang out between streams → discord.gg/your-invite' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                The editor also offers access levels (everyone up to broadcaster), shared and per-viewer
                cooldowns, and an "only while live" switch, all optional, all explained in the
                <a href="/guides/commands">commands guide</a>.
            </p>`,
        },
      ],
    },
    {
      id: 'first-module',
      heading: 'Switch on your first module',
      note: 'Modules are bigger features with an on/off switch. Chat Alerts is already on for you.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Head to <strong>Modules</strong>. Each row is one feature with its own switch, and
                clicking a row opens its settings. <strong>Chat Alerts</strong> (follows, subs, cheers,
                raids) is already working for you out of the box. <strong>AutoMod</strong> is a
                Premium beta: a free channel gets only the small safety floor (hate slurs and
                IP-grabber links), and the full checks need Premium. Two modules never show a switch
                at all: <strong>Counters</strong> and
                <strong>Stream Management</strong> (the commands behind <code>!title</code>,
                <code>!game</code> and <code>!marker</code>) are always on.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesGrid',
          path: '/modules',
          caption: 'The Modules page: a category rail on the left, one row per module, and a switch.',
          labels: { dot3: '', dot4: '', dot5: '', dot6: '' },
          notes: [
            { n: 1, text: 'The Categories rail. Click a group and the list scrolls to it.' },
            { n: 2, text: 'A row is one module: its name, one line about it, the commands it brings, and the switch. Chat Alerts starts on; most others wait for you.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Nothing here is dangerous to poke: every module can be switched off as quickly as it
                came on, and its settings are kept for the next time. The full tour of what each one
                does lives in the <a href="/guides/modules">modules handbook</a>.
            </p>`,
        },
      ],
    },
    {
      id: 'go-live',
      heading: 'Go live with confidence',
      note: 'A two-minute pre-stream checklist, then the bot takes the night shift.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>Before your next stream, a quick rehearsal:</p>
            <ol>
                <li>Type your new command in chat yourself: the bot answers you like any viewer.</li>
                <li>Skim the <strong>Chat Alerts</strong> messages and make them sound like you.</li>
                <li>Raided often? Switch on <strong>Auto Shoutout</strong> so incoming raiders get greeted even when you're mid-game.</li>
            </ol>
            <p>
                That's the whole setup. The bot moderates, greets, and answers on its own from here,
                and everything you just did can be changed mid-stream from your phone.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Next up</b>
                Make your commands smart: <a href="/guides/commands">Commands &amp; variables</a> shows
                how one line like <code>Welcome, &lbrace;user&rbrace;!</code> greets every viewer by name.`,
        },
        {
          kind: 'widget',
          name: 'Checklist',
          labels: {
            heading: 'Your first-hour checklist',
            reset: 'Clear',
          },
          props: {
            storageKey: 'guides.getting-started.checklist',
            items: [
              'Sign in at <a href="https://dashboard.itsbagelbot.com/auth/login" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a> and mod the bot with <code>/mod ItsBagelBot</code>.',
              'Create one command, like <code>!discord</code> or <code>!socials</code>.',
              'Open <strong>Modules</strong> and rewrite the <strong>Chat Alerts</strong> messages so they sound like you.',
              'Switch on <strong>Local Time</strong> and set your timezone, so <code>!time</code> answers correctly.',
              'Add a mod on <strong>Settings</strong>, so someone else can help run the bot.',
              'Type your new command in chat yourself and check the reply.',
              'Try <code>!uptime</code> once you go live.',
            ],
          },
        },
      ],
    },
    {
      id: 'bot-status',
      heading: 'If the bot is not in chat',
      note: 'Bot status on the Overview page tells you what to do.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Open <strong>Overview</strong> and look at <strong>Bot status</strong>. It shows one
                state and the buttons for it. A healthy card shows <strong>Restart</strong> and
                <strong>Disconnect</strong>. <strong>Enable</strong> switches a bot that you turned off
                back on. <strong>Reconnect</strong> takes you to <strong>Settings</strong>, where you
                choose <strong>Reconnect Twitch</strong> to sign your Twitch account in again after
                Twitch took the bot's access away. A ban needs a different fix, in Twitch chat: type
                <code>/unban ItsBagelBot</code>. You or one of your moderators can do it. You can also
                remove the bot from the banned list in your Twitch Creator Dashboard moderation
                settings.
            </p>`,
        },
        {
          kind: 'table',
          head: ['What you see', 'What it means', 'What to do'],
          caption: 'If something goes wrong',
          rows: [
            ['Not connected: "The bot has no Twitch authorization for your channel."', 'The bot is not signed in to your channel.', 'Select <strong>Connect</strong>.'],
            ['Disabled by you: "Your bot is switched off. Turn it back on to rejoin chat."', 'You switched the bot off.', 'Select <strong>Enable</strong>.'],
            ['Reconnect needed or Twitch access lost: "Twitch revoked the bot’s access to your channel. This happens after a password change or if the app was disconnected on Twitch. Reconnect your Twitch account to bring the bot back."', 'Twitch no longer lets the bot act for your channel.', 'Select <strong>Reconnect</strong>. It takes you to <strong>Settings</strong>. There, choose <strong>Reconnect Twitch</strong>.'],
            ['Bot banned from your chat: "ItsBagelBot is banned from your chat. To unban it, type /unban ItsBagelBot in your Twitch chat (you or one of your moderators can), or remove it from the banned list in your Twitch Creator Dashboard moderation settings. It rejoins on its own after that." or "ItsBagelBot is banned from your chat. To unban it, type /unban ItsBagelBot in your Twitch chat (you or one of your moderators can), or remove it from the banned list in your Twitch Creator Dashboard moderation settings. Then select I unbanned it, check again."', 'Your chat banned the bot, so it cannot join.', 'Type <code>/unban ItsBagelBot</code> in your Twitch chat. It rejoins on its own. If the button says <strong>I unbanned it, check again</strong>, select it. If it says <strong>Enable and verify unban</strong>, select that.'],
            ['"ItsBagelBot is still banned from your chat. Type /unban ItsBagelBot in your Twitch chat, then select Enable and verify unban."', 'The check found the ban still in place.', 'Unban the bot, then select <strong>Enable and verify unban</strong>.'],
            ['Connection check failed: "Access could not be confirmed. Retry when the problem is fixed."', 'The bot could not confirm its access.', 'Fix the problem, then select <strong>Retry connection check</strong>.'],
            ['Dormant: "The bot wakes automatically when an enabled feature is used. Your settings are preserved."', 'The bot is resting until something needs it.', 'Nothing. It wakes on its own.'],
            ['Timed out: "Normal bot work pauses until the timeout ends and access is verified."', 'The bot is timed out in your chat.', 'Wait for the timeout to end. The bot then checks its access again.'],
            [
              "Connected · idle: \"The bot is connected but not replying in chat.\"",
              "The bot is in your chat but is not answering.",
              "Select <strong>Enable above</strong>.",
            ],
            [
              "\"The bot lost its chat connection, so viewers might not get replies.\" or \"The bot lost its chat connection. Fix it in Settings → Reconnect\"",
              "The bot's chat connection dropped.",
              "Select <strong>Reconnect</strong>. It takes you to <strong>Settings</strong>. There, choose <strong>Reconnect Twitch</strong>.",
            ],
            [
              "Reconnecting…: \"Checking access before restoring operation.\"",
              "The bot is checking its access before it comes back.",
              "Wait a moment. The state changes on its own.",
            ],
            [
              "\"Twitch authorization also needs reconnecting.\"",
              "A second Twitch permission needs renewing.",
              "Select <strong>Reconnect</strong> next to the message, then choose <strong>Reconnect Twitch</strong> in <strong>Settings</strong>.",
            ],
            [
              "Checking connection… or Connecting…",
              "The dashboard is still reading the bot's state.",
              "Wait a moment.",
            ],
            [
              "Connection status unavailable",
              "The dashboard could not read the bot's state.",
              "Reload the page in a moment.",
            ],
          ],
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
