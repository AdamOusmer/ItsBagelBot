// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "getting-started" guide in en. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/getting-started.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'connect.b0.html': `
            <p>
                Head to <a href="https://dashboard.itsbagelbot.com/auth/login" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a>
                and sign in with your Twitch account. Twitch shows you exactly what the bot is allowed
                to do before you approve anything, and that's the whole setup. The moment you're in,
                ItsBagelBot joins your chat and sits quietly until it's spoken to.
            </p>`,
    'connect.b1.html': `
            <p>
                First time in, a short walkthrough greets you: accept the terms, pick your console
                language, then mod the bot by typing <code>/mod ItsBagelBot</code> in your own chat.
                That last one matters more than it looks: without mod status, Twitch silences the bot
                the moment your chat goes follower-only or sub-only, and it just looks broken.
            </p>`,
    'connect.b2.caption': 'What your chat sees, about two seconds after you sign in.',
    'connect.b2.lines.0.text': 'itsbagelbot joined #your_channel',
    'connect.b2.lines.1.name': 'maya_live',
    'connect.b2.lines.1.text': 'oh a new bot, hi!',
    'connect.b2.title': '#your_channel',
    'connect.b3.html': `
                <b>Good to know</b>
                The bot never speaks unprompted. Until you create commands or enable modules, joining
                is the only thing it does: your chat stays exactly as it was.`,
    'connect.heading': 'Connect your channel',
    'connect.note': 'One Twitch sign-in. No API keys, no config files, nothing to install.',
    'first-command.b0.html': `
            <p>
                Open <strong>Commands</strong> and press <strong>New command</strong>. A command needs
                exactly two things: a <strong>name</strong> (what viewers type after the
                <code>!</code>) and a <strong>response</strong> (what the bot says back). Let's make
                the classic:
            </p>`,
    'first-command.b1.caption': 'The Commands page: your list on the left, the editor docked on the right.',
    'first-command.b1.notes.0.text': 'New command opens the editor, docked beside the list on desktop (a bottom sheet on phones).',
    'first-command.b1.notes.1.text': 'The name, without the “!” (the dashboard adds it in chat). Lowercase, no spaces.',
    'first-command.b1.notes.2.text': 'The response. Plain text is fine; the next guide shows the variables that make it smart.',
    'first-command.b1.notes.3.text': 'Create saves it instantly, and the command goes live in chat right after.',
    'first-command.b2.caption': 'Thirty seconds later, in chat.',
    'first-command.b2.lines.0.name': 'maya_live',
    'first-command.b2.lines.0.text': '!discord',
    'first-command.b2.lines.1.text': 'Come hang out between streams → discord.gg/your-invite',
    'first-command.b2.title': '#your_channel',
    'first-command.b3.html': `
            <p>
                The editor also offers access levels (everyone up to broadcaster), a cooldown, and a
                "only while live" switch, all optional, all explained in the
                <a href="/guides/commands">commands guide</a>.
            </p>`,
    'first-command.heading': 'Create your first command',
    'first-command.note': 'A name and a response. Everything else is optional.',
    'first-module.b0.html': `
            <p>
                Head to <strong>Modules</strong>. Each tile is one feature with its own switch, and
                clicking a tile opens its settings. Two are already working for you out of the box:
                <strong>Chat Alerts</strong> (follows, subs, cheers, raids) and <strong>AutoMod</strong>
                (the layered moderation you read about on the homepage; it runs quietly without a
                tile on this grid). Two more never show a switch at all: <strong>Counters</strong> and
                <strong>Stream Management</strong> (the commands behind <code>!title</code>,
                <code>!game</code> and <code>!marker</code>) are always on.
            </p>`,
    'first-module.b1.caption': 'The Modules page: a category rail on the left, tiles with a Configure button and a switch.',
    'first-module.b1.labels.dot3': '',
    'first-module.b1.labels.dot4': '',
    'first-module.b1.labels.dot5': '',
    'first-module.b1.labels.dot6': '',
    'first-module.b1.notes.0.text': 'The Categories rail. Click a group and the grid scrolls to it.',
    'first-module.b1.notes.1.text': 'A tile is one module: its name, its category, one line about it, Configure, and the switch. Chat Alerts starts on; most others wait for you.',
    'first-module.b2.html': `
            <p>
                Nothing here is dangerous to poke: every module can be switched off as quickly as it
                came on, and its settings are kept for the next time. The full tour of what each one
                does lives in the <a href="/guides/modules">modules handbook</a>.
            </p>`,
    'first-module.heading': 'Switch on your first module',
    'first-module.note': 'Modules are bigger features with an on/off switch. Two are already on for you.',
    'go-live.b0.html': `
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
    'go-live.b1.html': `
                <b>Next up</b>
                Make your commands smart: <a href="/guides/commands">Commands &amp; variables</a> shows
                how one line like <code>Welcome, &lbrace;user&rbrace;!</code> greets every viewer by name.`,
    'go-live.b2.labels.heading': 'Your first-hour checklist',
    'go-live.b2.labels.reset': 'Clear',
    'go-live.b2.props.items.0': 'Sign in at <a href="https://dashboard.itsbagelbot.com/auth/login" target="_blank" rel="noopener noreferrer">dashboard.itsbagelbot.com</a> and mod the bot with <code>/mod ItsBagelBot</code>.',
    'go-live.b2.props.items.1': 'Create one command, like <code>!discord</code> or <code>!socials</code>.',
    'go-live.b2.props.items.2': 'Open <strong>Modules</strong> and rewrite the <strong>Chat Alerts</strong> messages so they sound like you.',
    'go-live.b2.props.items.3': 'Switch on <strong>Local Time</strong> and set your timezone, so <code>!time</code> answers correctly.',
    'go-live.b2.props.items.4': 'Add a mod on <strong>Settings</strong>, so someone else can help run the bot.',
    'go-live.b2.props.items.5': 'Type your new command in chat yourself and check the reply.',
    'go-live.b2.props.items.6': 'Try <code>!uptime</code> once you go live.',
    'go-live.heading': 'Go live with confidence',
    'go-live.note': 'A two-minute pre-stream checklist, then the bot takes the night shift.',
    'meta.card.chips.0': 'connect',
    'meta.card.chips.1': 'dashboard tour',
    'meta.card.chips.2': 'first module',
    'meta.card.description': 'From "Add to Twitch" to your first live stream with the bot: connect the channel, find your way around the dashboard, and switch on the first tools.',
    'meta.card.meta': '7 min · 5 steps',
    'meta.card.title': 'Getting started',
    'meta.description': 'Set up ItsBagelBot in minutes: connect your Twitch channel, tour the dashboard, create your first command, and switch on your first module.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Getting started',
    'meta.lead': 'From “Add to Twitch” to your first stream with the bot in chat. About seven minutes, most of it reading.',
    'meta.minutes': '7 min read',
    'meta.title': 'Getting started - ItsBagelBot Guides',
    'tour.b0.html': `
            <p>
                The dashboard is one page at a time, and its whole navigation is the floating dock
                at the bottom of the screen, the same on desktop and phone. <strong>Overview</strong>
                is your landing page, <strong>Commands</strong> is where custom chat commands live, and
                <strong>Modules</strong> holds every bigger feature. <strong>Discord</strong> is its own
                page for connecting a Discord server (premium beta). Billing and Settings do what they
                say on the tin.
            </p>`,
    'tour.b1.caption': 'The Overview page. Navigation lives in the floating dock at the bottom.',
    'tour.b1.notes.0.text': 'The dock is the whole navigation, on every screen size: Overview, Commands, Modules, Discord, Billing, Settings.',
    'tour.b1.notes.1.text': 'Bot status: whether ItsBagelBot is sitting in your chat right now, and the one button to fix it if not.',
    'tour.b1.notes.2.text': 'Quick actions: the two things you will do most, one tap away.',
    'tour.b1.notes.3.text': 'Your most-used commands live here too, one click from editing.',
    'tour.b2.html': `
            <p>
                Everything on the dashboard saves the moment you confirm it: there's no "deploy" step,
                and changes usually reach your chat within seconds.
            </p>`,
    'tour.heading': 'Find your way around',
    'tour.note': 'Six stops in the floating dock. You will spend most of your time in two of them.',
};

export default strings;
