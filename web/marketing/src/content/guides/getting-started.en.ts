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
      meta: '7 min · 5 steps',
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
                First time in, a short walkthrough greets you: accept the terms, pick your console
                language, then mod the bot by typing <code>/mod ItsBagelBot</code> in your own chat.
                That last one matters more than it looks: without mod status, Twitch silences the bot
                the moment your chat goes follower-only or sub-only, and it just looks broken.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'What your chat sees, about two seconds after you sign in.',
          lines: [
            { who: 'system', text: 'itsbagelbot joined #your_channel' },
            { who: 'viewer', name: 'maya_live', text: 'oh a new bot, hi!' },
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
            { who: 'viewer', name: 'maya_live', text: '!discord' },
            { who: 'bot', text: 'Come hang out between streams → discord.gg/your-invite' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                The editor also offers access levels (everyone up to broadcaster), a cooldown, and a
                "only while live" switch, all optional, all explained in the
                <a href="/guides/commands">commands guide</a>.
            </p>`,
        },
      ],
    },
    {
      id: 'first-module',
      heading: 'Switch on your first module',
      note: 'Modules are bigger features with an on/off switch. Two are already on for you.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Head to <strong>Modules</strong>. Each row is one feature with its own switch, and
                clicking a row opens its settings. Two are already working for you out of the box:
                <strong>Chat Alerts</strong> (follows, subs, cheers, raids) and <strong>AutoMod</strong>
                (the layered moderation you read about on the homepage; it runs quietly without a
                row in this list). Two more never show a switch at all: <strong>Counters</strong> and
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
  ],
};

export default guide;
