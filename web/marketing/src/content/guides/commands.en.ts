// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'commands',
  meta: {
    title: 'Commands & variables - ItsBagelBot Guides',
    description:
      'Master ItsBagelBot custom commands: every supported variable ({user}, {random}, {counter} and more), multi-line replies, chat actions, cooldowns and access levels.',
    eyebrow: 'Guide',
    heading: 'Commands & variables',
    lead: 'Commands that greet people by name, roll dice, and count your wins. No code: just braces.',
    minutes: '16 min read',
    card: {
      title: 'Commands & variables',
      description:
        'Build commands that greet people by name, roll dice, and count wins. Every variable the bot understands, explained with live-looking chat examples.',
      meta: '16 min · 14 steps',
      chips: ['{user}', '{random}', '{counter:…}', '!cmd'],
    },
  },
  sections: [
    {
      id: 'anatomy',
      heading: 'The anatomy of a command',
      note: 'Two required fields, five optional dials. Learn them once, reuse them forever.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A custom command is a question your viewers can ask (<code>!hug</code>) and the answer
                your bot gives. The editor has seven fields; only Name and Response are required.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'CommandEditor',
          path: '/commands',
          caption: 'The command editor as it docks beside your list, every field annotated.',
          notes: [
            { n: 1, text: 'Name: what viewers type after the “!”. Lowercase, one word.' },
            { n: 2, text: 'Alternate names: extra triggers for the same command (!hug and !cuddle can be one command).' },
            { n: 3, text: 'Response: what the bot says. Up to 5 lines; every line is its own chat message.' },
            { n: 4, text: 'The chat rehearsal acts your response out with sample values before you save. The command builder has the same one.' },
            { n: 5, text: 'Access and cooldown: who can use it, and how many quiet seconds follow each use.' },
            { n: 6, text: 'Only while live parks the command when the stream is offline; Active is the on/off switch.' },
            { n: 7, text: 'Data source: inserts a value fetched from a saved API definition instead of a variable.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                The Data source chip inserts <code>&#123;urlfetch:name&#125;</code>, a value pulled from
                an API you saved yourself. The <a href="/guides/data-sources">Data sources guide</a>
                walks through saving your first one.`,
        },
      ],
    },
    {
      id: 'create',
      heading: 'Two ways to create one',
      note: 'The dashboard editor, or a one-line chat message. Both land in the same place.',
      blocks: [
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: '!cmd add, edit and remove are for the broadcaster and moderators.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: "!cmd add hype LET'S GOOO 🎉" },
            { who: 'bot', text: '@mod_sam the command hype has been added' },
            { who: 'viewer', name: 'maya_live', text: '!hype' },
            { who: 'bot', text: "LET'S GOOO 🎉" },
          ],
        },
        {
          kind: 'prose',
          html: `
            <ul>
                <li><code>!cmd add &lt;name&gt; &lt;response&gt;</code> creates a command.</li>
                <li><code>!cmd edit &lt;name&gt; &lt;response&gt;</code> replaces its response.</li>
                <li><code>!cmd remove &lt;name&gt;</code> deletes it.</li>
            </ul>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                Chat is fast for quick one-liners; the dashboard shows the extra dials (access,
                cooldown, aliases) and a live preview. Use whichever is closer to your hands.`,
        },
      ],
    },
    {
      id: 'variables',
      heading: 'Variables: the smart parts',
      note: 'Curly braces are placeholders the bot fills in when it replies. All of them are in the variables reference.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Write <code>&#123;user&#125;</code> in a response, and the bot swaps it for the name of
                whoever ran the command. That is a variable: a placeholder the bot fills in at the
                moment it replies. Every variable it understands, with its exact syntax and a worked
                example, lives in the <a href="/guides/variables">variables reference</a>.
            </p>
            <p>
                Add a pipe and some text inside the braces to give a variable something to say when it
                comes back empty: <code>hug &#123;touser|everyone&#125;</code> hugs whoever you named,
                or everyone when you named nobody.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'One command, two very different sentences: !hug alone vs !hug alex.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!hug' },
            { who: 'bot', text: 'maya_live gives everyone a warm bagel hug 🥯' },
            { who: 'viewer', name: 'maya_live', text: '!hug alex' },
            { who: 'bot', text: 'maya_live gives alex a warm bagel hug 🥯' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>See every variable</b>
                The <a href="/guides/variables">variables reference</a> lists syntax, examples, and
                which module each one needs, grouped the way you use them.`,
        },
      ],
    },
    {
      id: 'multiline',
      heading: 'Multiple lines & chat actions',
      note: 'Each line becomes its own chat message. A line can also announce, shout out, or pin.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A response can hold up to <strong>5 lines</strong>, and each line is sent as its own
                chat message, top to bottom. And a line that <em>starts</em> with one of these verbs
                becomes a native Twitch action instead of a plain message:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Line starts with', 'What happens'],
          rows: [
            ['<code>/me</code>', 'Italic "action" message, classic IRC style.'],
            ['<code>/announce</code>', 'A highlighted Twitch announcement. Color variants: <code>/announceblue</code>, <code>/announcegreen</code>, <code>/announceorange</code>, <code>/announcepurple</code>.'],
            ['<code>/shoutout</code>', 'A native Twitch shoutout to the first name on the line.'],
            ['<code>/pin</code>', 'Sends the message and pins it until the stream ends.'],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'A two-line command: an announcement, then a normal message.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!giveaway' },
            { who: 'system', text: 'announcement · Giveaway is LIVE! Type !enter to join.' },
            { who: 'bot', text: 'Winner picked at the top of the hour. Good luck! 🍀' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Built-in safety</b>
                Viewer-supplied text (<code>&#123;args&#125;</code>, <code>&#123;touser&#125;</code>)
                is scrubbed before it lands in the message, so nobody can sneak a
                <code>/ban</code> or <code>/timeout</code> into your command's output.`,
        },
      ],
    },
    {
      id: 'rules',
      heading: 'The rules of the road',
      note: "Names, limits, cooldowns, and access levels. Everything the editor will and won't accept.",
      blocks: [
        {
          kind: 'prose',
          html: `<p>The editor checks all of this for you and says what's wrong in plain words. For reference:</p>`,
        },
        {
          kind: 'table',
          head: ['Field', 'The rule'],
          rows: [
            ['Name', '1 to 64 characters, no spaces, and leave out the "!" (chat adds it). Stored lowercase: <code>!Hug</code> and <code>!hug</code> are the same command.'],
            ['Alternate names', 'Up to 25, each following the same rules as the name.'],
            ['Response', 'Up to 5 lines, each up to 500 characters (one chat message per line).'],
            ['Cooldown', '0 to 86400 seconds. It is shared by the whole chat: after anyone uses the command, everyone waits.'],
            ['Access', 'Minimum rank, in order: everyone, subscribers, VIPs, moderators, lead moderators, broadcaster. Each level includes everyone above it.'],
            ['Restrict to one user', "Optionally lock a command to a single Twitch account; that overrides the access level entirely. Perfect for one friend's personal command."],
          ],
        },
      ],
    },
    {
      id: 'builder',
      heading: 'Skip the typing: the builder',
      note: 'Compose a command by clicking, preview it live, send it to your dashboard.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Everything in this guide is baked into the
                <a href="/command-builder">command builder</a>: pick variables from a labeled list
                instead of memorizing them, watch a live chat rehearsal as you type, and when it reads
                right, send it straight to your dashboard, where one press confirms and creates it. It is the fastest
                way to go from idea to working command.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Try it now</b>
                <a href="/command-builder">Open the command builder</a>`,
        },
      ],
    },
  ],
};

export default guide;
