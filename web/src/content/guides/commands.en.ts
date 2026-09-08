// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "commands" guide in en. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/commands.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'anatomy.b0.html': `
            <p>
                A custom command is a question your viewers can ask (<code>!hug</code>) and the answer
                your bot gives. The editor has seven fields; only Name and Response are required.
            </p>`,
    'anatomy.b1.caption': 'The command editor as it docks beside your list, every field annotated.',
    'anatomy.b1.notes.0.text': 'Name: what viewers type after the “!”. Lowercase, one word.',
    'anatomy.b1.notes.1.text': 'Alternate names: extra triggers for the same command (!hug and !cuddle can be one command).',
    'anatomy.b1.notes.2.text': 'Response: what the bot says. Up to 5 lines; every line is its own chat message.',
    'anatomy.b1.notes.3.text': 'The chat rehearsal acts your response out with sample values before you save. The command builder has the same one.',
    'anatomy.b1.notes.4.text': 'Access and cooldown: who can use it, and how many quiet seconds follow each use.',
    'anatomy.b1.notes.5.text': 'Only while live parks the command when the stream is offline; Active is the on/off switch.',
    'anatomy.b1.notes.6.text': 'Data source: inserts a value fetched from a saved API definition instead of a variable.',
    'anatomy.b2.html': `
                <b>Tip</b>
                The Data source chip inserts <code>&#123;urlfetch:name&#125;</code>, a value pulled from
                an API you saved yourself. The <a href="/guides/data-sources">Data sources guide</a>
                walks through saving your first one.`,
    'anatomy.heading': 'The anatomy of a command',
    'anatomy.note': 'Two required fields, five optional dials. Learn them once, reuse them forever.',
    'builder.b0.html': `
            <p>
                Everything in this guide is baked into the
                <a href="/command-builder">command builder</a>: pick variables from a labeled list
                instead of memorizing them, watch a live chat rehearsal as you type, and when it reads
                right, send it straight to your dashboard, where one press confirms and creates it. It is the fastest
                way to go from idea to working command.
            </p>`,
    'builder.b1.html': `
                <b>Try it now</b>
                <a href="/command-builder">Open the command builder</a>`,
    'builder.heading': 'Skip the typing: the builder',
    'builder.note': 'Compose a command by clicking, preview it live, send it to your dashboard.',
    'create.b0.html': `
            <h3>From the dashboard</h3>
            <p>
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands</a>,
                click <strong>New command</strong>, fill in the name and the response, then <strong>Create</strong>.
                It usually answers in chat within seconds. Edits work the same way: click any command row,
                change it, save.
            </p>
            <h3>From chat, with !cmd</h3>
            <p>
                You and your moderators can also manage commands without leaving chat, mid-stream.
                Any moderator can run it: <code>!cmd</code> doesn't need the lead moderator promotion
                that some other built-ins ask for.
            </p>`,
    'create.b1.caption': '!cmd add, edit and remove are for the broadcaster and moderators.',
    'create.b1.lines.0.name': 'mod_sam',
    'create.b1.lines.0.text': "!cmd add hype LET'S GOOO 🎉",
    'create.b1.lines.1.text': '@mod_sam the command hype has been added',
    'create.b1.lines.2.name': 'maya_live',
    'create.b1.lines.2.text': '!hype',
    'create.b1.lines.3.text': "LET'S GOOO 🎉",
    'create.b1.title': '#your_channel',
    'create.b2.html': `
            <ul>
                <li><code>!cmd add &lt;name&gt; &lt;response&gt;</code> creates a command.</li>
                <li><code>!cmd edit &lt;name&gt; &lt;response&gt;</code> replaces its response.</li>
                <li><code>!cmd remove &lt;name&gt;</code> deletes it.</li>
            </ul>`,
    'create.b3.html': `
                <b>Tip</b>
                Chat is fast for quick one-liners; the dashboard shows the extra dials (access,
                cooldown, aliases) and a live preview. Use whichever is closer to your hands.`,
    'create.heading': 'Two ways to create one',
    'create.note': 'The dashboard editor, or a one-line chat message. Both land in the same place.',
    'dynamic.b0.html': `
            <h3>&#123;random&#125;: dice</h3>
            <p>
                <code>&#123;random&#125;</code> becomes a whole number from 1 to 100. Pick your own
                range with <code>&#123;random:1-6&#125;</code> (both ends included).
            </p>
            <h3>&#123;choice:…&#125;: a coin with as many sides as you like</h3>
            <p>
                <code>&#123;choice:yes,no,ask again later&#125;</code> picks one option from your
                comma-separated list, fresh every time.
            </p>
            <h3>&#123;counter:…&#125;: a number that remembers</h3>
            <p>
                <code>&#123;counter:falls&#125;</code> adds 1 to a counter named "falls" and shows the
                new total. The count survives streams, so <code>!fall</code> can track your tumbles all
                year. Every counter has a scope, chosen when it's created, that decides whose number
                it is: one shared total for the channel, one per viewer, one pooled per command or
                reward, or one per viewer per command. The <a href="/guides/counters">counters
                guide</a> walks through all four with examples.
            </p>`,
    'dynamic.b1.caption': 'All three in the wild.',
    'dynamic.b1.lines.0.name': 'maya_live',
    'dynamic.b1.lines.0.text': '!roll',
    'dynamic.b1.lines.1.text': 'maya_live rolls a 73 out of 100 🎲',
    'dynamic.b1.lines.2.name': 'alex',
    'dynamic.b1.lines.2.text': '!fall',
    'dynamic.b1.lines.3.text': 'your_channel has fallen 128 times. A new record of grace.',
    'dynamic.b1.title': '#your_channel',
    'dynamic.b2.html': `
                <b>Counters need Loyalty Points</b>
                Counters are stored by the <a href="/guides/modules">Loyalty Points module</a>. Switch
                it on first, or the <code>&#123;counter:…&#125;</code> stays as literal text. Your mods
                can manage counts in chat with <code>!counter set</code>, <code>!counter reset</code>
                and friends.`,
    'dynamic.heading': 'Dice, choices, and counters',
    'dynamic.note': 'Three variables that change every time: random numbers, random picks, and counters that remember.',
    'meta.card.chips.0': '{user}',
    'meta.card.chips.1': '{random}',
    'meta.card.chips.2': '{counter:…}',
    'meta.card.chips.3': '!cmd',
    'meta.card.description': 'Build commands that greet people by name, roll dice, and count wins. Every variable the bot understands, explained with live-looking chat examples.',
    'meta.card.meta': '9 min · 7 steps',
    'meta.card.title': 'Commands & variables',
    'meta.description': 'Master ItsBagelBot custom commands: every supported variable ({user}, {random}, {counter} and more), multi-line replies, chat actions, cooldowns and access levels.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Commands & variables',
    'meta.lead': 'Commands that greet people by name, roll dice, and count your wins. No code: just braces.',
    'meta.minutes': '9 min read',
    'meta.title': 'Commands & variables - ItsBagelBot Guides',
    'multiline.b0.html': `
            <p>
                A response can hold up to <strong>5 lines</strong>, and each line is sent as its own
                chat message, top to bottom. And a line that <em>starts</em> with one of these verbs
                becomes a native Twitch action instead of a plain message:
            </p>`,
    'multiline.b1.head.0': 'Line starts with',
    'multiline.b1.head.1': 'What happens',
    'multiline.b1.rows.0.0': '<code>/me</code>',
    'multiline.b1.rows.0.1': 'Italic "action" message, classic IRC style.',
    'multiline.b1.rows.1.0': '<code>/announce</code>',
    'multiline.b1.rows.1.1': 'A highlighted Twitch announcement. Color variants: <code>/announceblue</code>, <code>/announcegreen</code>, <code>/announceorange</code>, <code>/announcepurple</code>.',
    'multiline.b1.rows.2.0': '<code>/shoutout</code>',
    'multiline.b1.rows.2.1': 'A native Twitch shoutout to the first name on the line.',
    'multiline.b1.rows.3.0': '<code>/pin</code>',
    'multiline.b1.rows.3.1': 'Sends the message and pins it until the stream ends.',
    'multiline.b2.caption': 'A two-line command: an announcement, then a normal message.',
    'multiline.b2.lines.0.name': 'maya_live',
    'multiline.b2.lines.0.text': '!giveaway',
    'multiline.b2.lines.1.text': 'announcement · Giveaway is LIVE! Type !enter to join.',
    'multiline.b2.lines.2.text': 'Winner picked at the top of the hour. Good luck! 🍀',
    'multiline.b2.title': '#your_channel',
    'multiline.b3.html': `
                <b>Built-in safety</b>
                Viewer-supplied text (<code>&#123;args&#125;</code>, <code>&#123;touser&#125;</code>)
                is scrubbed before it lands in the message, so nobody can sneak a
                <code>/ban</code> or <code>/timeout</code> into your command's output.`,
    'multiline.heading': 'Multiple lines & chat actions',
    'multiline.note': 'Each line becomes its own chat message. A line can also announce, shout out, or pin.',
    'rules.b0.html': "<p>The editor checks all of this for you and says what's wrong in plain words. For reference:</p>",
    'rules.b1.head.0': 'Field',
    'rules.b1.head.1': 'The rule',
    'rules.b1.rows.0.0': 'Name',
    'rules.b1.rows.0.1': '1 to 64 characters, no spaces, and leave out the "!" (chat adds it). Stored lowercase: <code>!Hug</code> and <code>!hug</code> are the same command.',
    'rules.b1.rows.1.0': 'Alternate names',
    'rules.b1.rows.1.1': 'Up to 25, each following the same rules as the name.',
    'rules.b1.rows.2.0': 'Response',
    'rules.b1.rows.2.1': 'Up to 5 lines, each up to 500 characters (one chat message per line).',
    'rules.b1.rows.3.0': 'Cooldown',
    'rules.b1.rows.3.1': '0 to 86400 seconds. It is shared by the whole chat: after anyone uses the command, everyone waits.',
    'rules.b1.rows.4.0': 'Access',
    'rules.b1.rows.4.1': 'Minimum rank, in order: everyone, subscribers, VIPs, moderators, lead moderators, broadcaster. Each level includes everyone above it.',
    'rules.b1.rows.5.0': 'Restrict to one user',
    'rules.b1.rows.5.1': "Optionally lock a command to a single Twitch account; that overrides the access level entirely. Perfect for one friend's personal command.",
    'rules.heading': 'The rules of the road',
    'rules.note': "Names, limits, cooldowns, and access levels. Everything the editor will and won't accept.",
    'variables.b0.html': `
            <p>
                Write <code>&#123;user&#125;</code> in a response, and the bot swaps it for the name of
                whoever ran the command. That single idea powers everything below. These are the
                people-and-place variables every custom command understands:
            </p>`,
    'variables.b1.head.0': 'Variable',
    'variables.b1.head.1': 'Becomes',
    'variables.b1.head.2': 'Example',
    'variables.b1.rows.0.0': '<code>&#123;user&#125;</code>',
    'variables.b1.rows.0.1': 'The viewer who used the command. <code>&#123;sender&#125;</code> is an older alias, same value.',
    'variables.b1.rows.0.2': 'maya_live',
    'variables.b1.rows.1.0': '<code>&#123;touser&#125;</code>',
    'variables.b1.rows.1.1': "The first word typed after the command, with any “@” removed. When nothing is typed, it falls back to the viewer's own name. <code>&#123;target&#125;</code> is the same thing.",
    'variables.b1.rows.1.2': 'alex',
    'variables.b1.rows.2.0': '<code>&#123;args&#125;</code>',
    'variables.b1.rows.2.1': 'Everything typed after the command, as one string. Empty when nothing was typed.',
    'variables.b1.rows.2.2': 'good luck on the exam',
    'variables.b1.rows.3.0': '<code>&#123;channel&#125;</code>',
    'variables.b1.rows.3.1': "Your channel's display name.",
    'variables.b1.rows.3.2': 'your_channel',
    'variables.b1.rows.4.0': '<code>&#123;urlfetch:name&#125;</code>',
    'variables.b1.rows.4.1': 'A value fetched from a web API you saved as a data source. Covered in the <a href="/guides/data-sources">Data sources guide</a>.',
    'variables.b1.rows.4.2': '22',
    'variables.b2.caption': 'One command, two very different sentences: !hug alone vs !hug alex.',
    'variables.b2.lines.0.name': 'maya_live',
    'variables.b2.lines.0.text': '!hug',
    'variables.b2.lines.1.text': 'maya_live gives maya_live a warm bagel hug 🥯',
    'variables.b2.lines.2.name': 'maya_live',
    'variables.b2.lines.2.text': '!hug alex',
    'variables.b2.lines.3.text': 'maya_live gives alex a warm bagel hug 🥯',
    'variables.b2.title': '#your_channel',
    'variables.b3.html': `
                <b>Watch out</b>
                A variable the bot doesn't recognize is left exactly as typed, braces included. If
                chat shows a literal <code>&#123;something&#125;</code>, check the spelling against the
                table above (or build the command in the <a href="/command-builder">builder</a>, which
                only offers real variables).`,
    'variables.heading': 'Variables: the smart parts',
    'variables.note': 'Curly braces are placeholders. The bot fills them in at the moment it replies.',
};

export default strings;
