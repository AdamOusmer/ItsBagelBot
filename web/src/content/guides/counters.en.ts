// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'counters',
  meta: {
    title: 'Counters - ItsBagelBot Guides',
    description:
      'How ItsBagelBot counters work: the {counter:name} token, the four scopes (whole channel, per user, per command, per user + command), managing them with !counter, and binding one to a channel-point reward.',
    eyebrow: 'Guide 04',
    heading: 'Counters',
    lead: "Track anything that happens more than once: deaths, hugs, redemptions. Choose how it's counted once, and the bot remembers forever.",
    minutes: '8 min read',
    card: {
      title: 'Counters',
      description:
        'Numbers the bot remembers forever: the {counter:name} token, the four scopes (channel, per user, per command, per user + command), and binding one to a channel-point reward.',
      meta: '8 min · 4 steps',
      chips: ['{counter:…}', 'scopes', '!counter', 'channel points'],
    },
  },
  sections: [
    {
      id: 'basics',
      heading: 'Counters: numbers that remember',
      note: 'One token, {counter:name}, adds 1 and shows the total. The count survives every stream.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A counter is a named number the bot remembers between streams. Write
                <code>&#123;counter:deaths&#125;</code> in a command's response, and every time the
                command runs, the bot adds 1 to a counter called "deaths" and shows the new total
                right there in its reply. No spreadsheet, no manual tally, just a token.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Two runs of the same !death command, one counter remembering both.',
          lines: [
            { who: 'viewer', name: 'alex', text: '!death' },
            { who: 'bot', text: 'your_channel has died 47 times.' },
            { who: 'viewer', name: 'maya_live', text: '!death' },
            { who: 'bot', text: 'your_channel has died 48 times.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Counters need Loyalty Points</b>
                Counters are stored by the <a href="/guides/modules">Loyalty Points module</a>. Switch
                it on first, or <code>&#123;counter:…&#125;</code> stays as literal text in chat.`,
        },
      ],
    },
    {
      id: 'scopes',
      heading: 'Four ways to count',
      note: 'Whole channel, per user, per command, or both. Pick one when you create the counter; it sticks.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Every counter has exactly one <strong>scope</strong>, chosen the moment it's created,
                and it stays that way for the counter's whole life. Scope answers one question: whose
                number is this?
            </p>`,
        },
        {
          kind: 'table',
          head: ['Scope', 'Chat word', "What's counted"],
          rows: [
            ['Whole channel', '<em>(default)</em>', 'One shared total, everyone adds to the same number.'],
            ['Per user', '<code>user</code>', 'Each viewer gets their own number.'],
            ['Per command or reward', '<code>command</code>', 'One shared total for a single command or reward, every viewer pooled together.'],
            ['Per user + command or reward', '<code>user+command</code>', 'Each viewer gets a separate number for each command or reward.'],
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Whole channel</h3>
            <p>
                Everyone bumps the same shared number. Perfect for a tally that belongs to the stream
                itself, not to any one viewer, like a running death count.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: "Doesn't matter who types !death, the channel's total keeps climbing.",
          lines: [
            { who: 'viewer', name: 'alex', text: '!death' },
            { who: 'bot', text: 'your_channel has died 47 times.' },
            { who: 'viewer', name: 'sam', text: '!death' },
            { who: 'bot', text: 'your_channel has died 48 times.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Per user</h3>
            <p>
                Each viewer gets a private number, untouched by anyone else's. Good for personal
                streaks: how many hugs someone has given, how many times they've won a game against
                you.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Same command, two viewers, two totally separate counts.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!hug' },
            { who: 'bot', text: 'maya_live has given 12 hugs.' },
            { who: 'viewer', name: 'alex', text: '!hug' },
            { who: 'bot', text: 'alex has given 1 hug.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Per command or reward</h3>
            <p>
                The newest scope: one pooled total for a single command or channel-point reward,
                with every viewer adding to the same number. Ideal for "how many times has this
                specific reward been redeemed, total?"
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: "Two different viewers, the reward's own running total either way.",
          lines: [
            { who: 'system', text: 'channel points · maya_live redeemed Hydrate!' },
            { who: 'bot', text: 'Hydrate! has been redeemed 301 times.' },
            { who: 'system', text: 'channel points · alex redeemed Hydrate!' },
            { who: 'bot', text: 'Hydrate! has been redeemed 302 times.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Per user + command or reward</h3>
            <p>
                Combines both: every viewer gets a separate number for each command or reward that
                shares this counter. Bind one counter to two commands and each viewer ends up with
                one bucket per command, not one shared bucket for both.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: "alex's count for !hug and their count for !highfive don't mix, even though both use the same counter.",
          lines: [
            { who: 'viewer', name: 'alex', text: '!hug' },
            { who: 'bot', text: 'alex has hugged 3 times.' },
            { who: 'viewer', name: 'alex', text: '!highfive' },
            { who: 'bot', text: 'alex has high-fived 1 time.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Locked once created</b>
                A counter's scope can't be changed afterward. Picked the wrong one? Delete it
                (<code>!counter delete name</code>, or the dashboard) and create it again; the old
                values don't carry over.`,
        },
      ],
    },
    {
      id: 'chat',
      heading: 'Managing counters from chat',
      note: 'Create, bump, set, reset, delete, or just ask, all without leaving chat.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                You and your moderators can create and manage counters without leaving chat, all
                under <code>!counter</code>:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Command', 'What it does'],
          rows: [
            ['<code>!counter create &lt;name&gt; [scope]</code>', 'Creates a counter. Scope word is <code>user</code>, <code>command</code> or <code>user+command</code>; leave it out for the whole channel.'],
            ['<code>!counter add &lt;name&gt; [amount]</code>', 'Adds 1, or the amount you give. Negative numbers subtract.'],
            ['<code>!counter set &lt;name&gt; &lt;value&gt;</code>', 'Sets the total outright.'],
            ['<code>!counter reset &lt;name&gt;</code>', 'Clears it back to zero. For per-user or per-command counters, this wipes every stored bucket.'],
            ['<code>!counter delete &lt;name&gt;</code>', 'Removes the counter entirely.'],
            ['<code>!counter list</code>', 'Lists every counter on the channel and its scope.'],
            ['<code>!counter &lt;name&gt;</code>', 'Shows the current value, same as reading it in chat.'],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Creating and bumping a counter, start to finish, without touching the dashboard.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: '!counter create deaths' },
            { who: 'bot', text: '@mod_sam counter deaths created (whole channel).' },
            { who: 'mod', name: 'mod_sam', text: '!counter add deaths 5' },
            { who: 'bot', text: '@mod_sam deaths is now 5.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Command scope, from chat</b>
                <code>!counter create hydrations command</code> makes a pooled per-command counter in
                one line, ready to drop into a command's response, or bind to a channel-point reward,
                as <code>&#123;counter:hydrations&#125;</code>.`,
        },
      ],
    },
    {
      id: 'dashboard',
      heading: 'The dashboard, and channel-point rewards',
      note: 'The Counters page for hands-on control, and how a reward binds to one.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                The <a href="https://dashboard.itsbagelbot.com/counters" target="_blank" rel="noopener noreferrer">Counters</a>
                page lists every counter on your channel and lets you create, adjust, and delete them
                by clicking instead of typing.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'NewCounter',
          path: '/counters',
          caption: 'New counter panel: name it, pick a scope, done. Existing rows get +/- steppers for whole-channel counters, or a values table for the others.',
          notes: [
            { n: 1, text: "Name: the same string you'd write inside {counter:…}." },
            { n: 2, text: 'Counts: the scope. Fixed once you create it.' },
            { n: 3, text: 'A plain-language reminder of where counters get bumped from.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Binding a counter to a channel-point reward</h3>
            <p>
                Every <a href="/guides/modules">Channel Points</a> reward can optionally keep a
                counter of its own, right from the reward's editor: type a counter name (new or
                existing), pick its scope, and every redemption bumps it. Use
                <code>&#123;counter&#125;</code> in the reward's chat reply to show the new total.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'RewardCounter',
          path: '/channelpoints',
          caption: "The reward editor's counter block: a toggle, a name, and the same four scopes.",
          notes: [
            { n: 1, text: "Off by default, most rewards don't need one." },
            { n: 2, text: 'Per user + reward is usually the right call: each viewer builds their own redemption count for this reward.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Try it in the builder</b>
                The <a href="/command-builder">command builder</a> can walk you through naming a
                counter and picking its scope, then hands you the finished
                <code>&#123;counter:…&#125;</code> token.
                <a href="/command-builder">Open the command builder →</a>`,
        },
      ],
    },
  ],
};

export default guide;
