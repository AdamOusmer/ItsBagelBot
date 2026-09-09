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
    minutes: '11 min read',
    card: {
      title: 'Commands & variables',
      description:
        'Build commands that greet people by name, roll dice, and count wins. Every variable the bot understands, explained with live-looking chat examples.',
      meta: '11 min · 9 steps',
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
      note: 'Curly braces are placeholders. The bot fills them in at the moment it replies.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Write <code>&#123;user&#125;</code> in a response, and the bot swaps it for the name of
                whoever ran the command. That single idea powers everything below. These are the
                people-and-place variables every custom command understands:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Variable', 'Becomes', 'Example'],
          rows: [
            ['<code>&#123;user&#125;</code>', 'The viewer who used the command. <code>&#123;sender&#125;</code> is an older alias, same value.', 'maya_live'],
            ['<code>&#123;touser&#125;</code>', "The first word typed after the command, with any “@” removed. When nothing is typed, it falls back to the viewer's own name. <code>&#123;target&#125;</code> is the same thing.", 'alex'],
            ['<code>&#123;args&#125;</code>', 'Everything typed after the command, as one string. Empty when nothing was typed.', 'alex good luck on the exam'],
            ['<code>&#123;1&#125;</code>, <code>&#123;2&#125;</code>, …', 'One word at a time: <code>&#123;1&#125;</code> is the first word typed after the command, <code>&#123;2&#125;</code> the second, up to <code>&#123;30&#125;</code>. A word nobody typed comes back empty.', 'alex'],
            ['<code>&#123;2:&#125;</code>', 'That word through to the end, as one string. Change the number to start somewhere else; <code>&#123;1:&#125;</code> is the whole thing.', 'good luck on the exam'],
            ['<code>&#123;userid&#125;</code>', "The viewer's Twitch user ID. It never changes, even when they rename themselves.", '48291057'],
            ['<code>&#123;user.login&#125;</code>', 'Their login in lowercase, which can differ from the display name <code>&#123;user&#125;</code> shows.', 'maya_live'],
            ['<code>&#123;command&#125;</code>', 'The name of the command that answered, without the “!”. Alternate names all report the main one.', 'hug'],
            ['<code>&#123;channel&#125;</code>', "Your channel's display name.", 'your_channel'],
            ['<code>&#123;urlfetch:name&#125;</code>', 'A value fetched from a web API you saved as a data source. Covered in the <a href="/guides/data-sources">Data sources guide</a>.', '22'],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'One command, two very different sentences: !hug alone vs !hug alex.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!hug' },
            { who: 'bot', text: 'maya_live gives maya_live a warm bagel hug 🥯' },
            { who: 'viewer', name: 'maya_live', text: '!hug alex' },
            { who: 'bot', text: 'maya_live gives alex a warm bagel hug 🥯' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Watch out</b>
                A variable the bot doesn't recognize is left exactly as typed, braces included. If
                chat shows a literal <code>&#123;something&#125;</code>, check the spelling against the
                table above (or build the command in the <a href="/command-builder">builder</a>, which
                only offers real variables).`,
        },
      ],
    },
    {
      id: 'fallbacks',
      heading: 'Defaults, for when a word is missing',
      note: 'A pipe inside a variable gives it something to say when it comes back empty.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                <code>Check out &#123;1&#125;!</code> reads badly when nobody typed a name: chat
                sees <em>Check out !</em>. Add a pipe and some text inside the braces, and that
                text stands in whenever the variable comes back empty:
                <code>Check out &#123;1|everyone&#125;!</code>.
            </p>
            <p>
                It works on any variable, including one that <em>looks</em> filled but is not:
                a <a href="/guides/data-sources">data source</a> that answered with nothing
                (<code>&#123;urlfetch:temp|offline&#125;</code>), or a mentioned viewer nobody
                named (<code>&#123;touser|chat&#125;</code>). The text after the pipe is plain
                text, not another variable.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'The same command, with and without a name after it.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!shoutout alex' },
            { who: 'bot', text: 'Go show alex some love 💛' },
            { who: 'viewer', name: 'maya_live', text: '!shoutout' },
            { who: 'bot', text: 'Go show everyone some love 💛' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>A default does not rescue a typo</b>
                The pipe only covers an <em>empty</em> value. A variable the bot does not
                recognize is still left exactly as typed, pipe and all: write
                <code>&#123;touser|chat&#125;</code> and chat sees a name, write
                <code>&#123;tousr|chat&#125;</code> and chat sees
                <code>&#123;tousr|chat&#125;</code>. That is on purpose, so a misspelling stays
                visible instead of hiding behind its own default forever.`,
        },
      ],
    },
    {
      id: 'dynamic',
      heading: 'Dice, choices, and counters',
      note: 'Three variables that change every time: random numbers, random picks, and counters that remember.',
      blocks: [
        {
          kind: 'prose',
          html: `
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
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'All three in the wild.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!roll' },
            { who: 'bot', text: 'maya_live rolls a 73 out of 100 🎲' },
            { who: 'viewer', name: 'alex', text: '!fall' },
            { who: 'bot', text: 'your_channel has fallen 128 times. A new record of grace.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Counters need Loyalty Points</b>
                Counters are stored by the <a href="/guides/modules">Loyalty Points module</a>. Switch
                it on first, or the <code>&#123;counter:…&#125;</code> stays as literal text. Your mods
                can manage counts in chat with <code>!counter set</code>, <code>!counter reset</code>
                and friends.`,
        },
        {
          kind: 'widget',
          name: 'Rehearsal',
        },
      ],
    },
    {
      id: 'utilities',
      heading: 'Small jobs the reply can do itself',
      note: 'Arithmetic, countdowns, repeats, and encoding text so it survives a URL.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A handful of variables work on what you type inside them rather than on who ran
                the command. They need no module and no setup: write one, and the bot works it
                out at the moment it replies.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Variable', 'Becomes', 'Example'],
          rows: [
            ['<code>&#123;math:1+2*3&#125;</code>', 'The answer to a small sum. Whole numbers with <code>+</code>, <code>-</code>, <code>*</code>, <code>/</code> and parentheses; multiplication and division go first, as in school. Division rounds toward zero, and dividing by zero comes back empty.', '7'],
            ['<code>&#123;countdown:2026-12-25&#125;</code>', 'How long until that date, in the same words as <code>!uptime</code>. Write the date as YYYY-MM-DD, or as a full timestamp with a time and a zone. Once it has passed, the count stops instead of turning around.', '3 days, 4 hours'],
            ['<code>&#123;countup:2020-01-01&#125;</code>', 'How long since that date. The same clock, read the other way.', '2 years, 3 months'],
            ['<code>&#123;repeat:3:bagel&#125;</code>', 'Your phrase, that many times, separated by spaces. Up to 20 times, and the whole run has to fit inside one chat line.', 'bagel bagel bagel'],
            ['<code>&#123;querystring&#125;</code>', 'Everything typed after the command, encoded so it can sit inside a web address. This is the one to put in a <a href="/guides/data-sources">data source</a> URL.', 'alex+good+luck'],
            ['<code>&#123;queryescape:hello world&#125;</code>', 'The same encoding, applied to text you write yourself. <code>&#123;pathescape:…&#125;</code> is its sibling for the path part of a URL, where a space becomes <code>%20</code> instead of <code>+</code>.', 'hello+world'],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'A countdown and a sum, in two ordinary commands.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!launch' },
            { who: 'bot', text: 'The new season drops in 3 days, 4 hours 🥯' },
            { who: 'viewer', name: 'alex', text: '!deaths' },
            { who: 'bot', text: 'That is 128 deaths, or 8 per hour. Flawless.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>They read what you typed, not what a variable holds</b>
                <code>&#123;math:&#123;counter:deaths&#125;+1&#125;</code> does not work: a variable
                inside another variable is not part of the language yet, so the sum sees the braces
                rather than the number and comes back empty. Anything these cannot work out comes
                back empty too, which is exactly when a default earns its keep:
                <code>&#123;math:1/0|no idea&#125;</code>.`,
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
