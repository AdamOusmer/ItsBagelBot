// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'counters',
  meta: {
    aiDoc: 'https://ai.itsbagelbot.com/features/engagement.md',
    title: 'Twitch Counters and Death Counter Setup | ItsBagelBot',
    description:
      'Create a Twitch death counter, use counters in commands and connect channel-point rewards. Follow examples for updating and displaying counts.',
    eyebrow: 'Guide',
    heading: 'Counters',
    lead: "Track anything that happens more than once: deaths, hugs, redemptions. Choose how it's counted once, and the bot remembers forever.",
    minutes: '8 min read',
    card: {
      title: 'Counters',
      description:
        'Numbers the bot remembers forever: counting from a command, the {counter:name} token, the four scopes (channel, per user, per command, per user + command), and binding one to a channel-point reward.',
      meta: '8 min · 4 steps',
      chips: ['{counter:…}', 'scopes', 'Bump a counter', 'channel points'],
    },
  },
  sections: [
    {
      id: 'basics',
      heading: 'Counters: numbers that remember',
      note: 'A command bumps the counter, the {counter:name} token shows it. The count survives every stream.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A counter is a named number the bot remembers between streams. Create one on the
                Counters page, then turn on <strong>Bump a counter</strong> in a custom command such as
                <code>!death</code> and pick the counter. Every use adds 1. Write
                <code>&#123;counter:deaths&#125;</code> in any response to show the current total.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: '!death adds 1 to the counter, and !deaths reads the total back with the token.',
          lines: [
            { who: 'viewer', name: 'MikanMeerkat', text: '!death' },
            { who: 'bot', text: 'The boss wins again.' },
            { who: 'viewer', name: 'SnackPackPanda', text: '!deaths' },
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
          head: ['Scope', "What's counted"],
          rows: [
            ['Whole channel', 'One shared total, everyone adds to the same number.'],
            ['Per user', 'Each viewer gets their own number.'],
            ['Per command or reward', 'One shared total for a single command or reward, every viewer pooled together.'],
            ['Per user + command or reward', 'Each viewer gets a separate number for each command or reward.'],
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
          caption: 'Whoever asks, the channel shows one shared total.',
          lines: [
            { who: 'viewer', name: 'MikanMeerkat', text: '!deaths' },
            { who: 'bot', text: 'your_channel has died 48 times.' },
            { who: 'viewer', name: 'Lotti', text: '!deaths' },
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
            { who: 'viewer', name: 'SnackPackPanda', text: '!hugs' },
            { who: 'bot', text: 'SnackPackPanda has given 12 hugs.' },
            { who: 'viewer', name: 'MikanMeerkat', text: '!hugs' },
            { who: 'bot', text: 'MikanMeerkat has given 1 hug.' },
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
          caption: "Every redemption adds to the reward's own total, and any viewer can read it back.",
          lines: [
            { who: 'system', text: 'channel points · SnackPackPanda redeemed Hydrate!' },
            { who: 'bot', text: 'Thanks for hydrating, SnackPackPanda!' },
            { who: 'viewer', name: 'MikanMeerkat', text: '!hydrations' },
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
          caption: "MikanMeerkat's count for !hugs and their count for !highfives don't mix, even though both use the same counter.",
          lines: [
            { who: 'viewer', name: 'MikanMeerkat', text: '!hugs' },
            { who: 'bot', text: 'MikanMeerkat has hugged 3 times.' },
            { who: 'viewer', name: 'MikanMeerkat', text: '!highfives' },
            { who: 'bot', text: 'MikanMeerkat has high-fived 1 time.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Locked once created</b>
                A counter's scope can't be changed afterward. Picked the wrong one? Delete it
                on the Counters page and create it again; the old values don't carry over.`,
        },
      ],
    },
    {
      id: 'chat',
      heading: 'Counting from chat',
      note: 'A command with Bump a counter adds 1, and moderators can type a number to add or remove several.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Counters are made on the Counters page (next section). To count from chat, create a
                custom command such as <code>!death</code>, turn on its <strong>Bump a counter</strong>
                option and pick the counter. Moderators can then choose the step:
            </p>`,
        },
        {
          kind: 'table',
          head: ['Command', 'What it does'],
          rows: [
            ['<code>!death</code>', 'Adds 1 to the counter, whoever types it.'],
            ['<code>!death 5</code> or <code>!death +5</code>', 'Adds 5. Only moderators, lead moderators and the broadcaster can pick the amount.'],
            ['<code>!death -1</code>', 'Removes 1 (a negative number subtracts), for the same roles.'],
            ['<code>!death 5</code> from a viewer', "The number is ignored, the counter still adds 1."],
            ['<code>!death 0</code>, <code>!death abc</code>, or a number past 1,000,000', 'Falls back to adding 1.'],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'The same command: a viewer adds 1, a moderator adds 5 in one go.',
          lines: [
            { who: 'viewer', name: 'SnackPackPanda', text: '!death' },
            { who: 'bot', text: 'The boss wins again.' },
            { who: 'mod', name: 'Ayshacinder', text: '!death 5' },
            { who: 'bot', text: 'The boss wins again.' },
          ],
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Set, reset and delete</b>
                Changing the total itself happens on the Counters page: set it to any number, reset
                it to zero, rename it or delete it. In a response, <code>&#123;counter:deaths&#125;</code>
                only shows the value and never changes it.`,
        },
        {
          kind: 'widget',
          name: 'CounterPlay',
          labels: {
            heading: 'Try it: bump the counter',
            windowTitle: '#your_channel',
            modName: 'Ayshacinder',
            addOne: '!death',
            subOne: '!death -1',
            addFive: '!death 5',
            viewerName: 'SnackPackPanda',
            viewerCommand: '!deaths',
            viewerReplyTemplate: 'Oh no, {value} deaths so far.',
            modReplyTemplate: 'The boss wins again.',
          },
          props: { counterName: 'deaths', start: 12 },
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
                page lists every counter on your channel. It is where you create, set, reset, rename
                and delete them.
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
                <code>&#123;counter&#125;</code> in the reward's chat reply to show the current total.
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
                <a href="/command-builder">Open the command builder</a>`,
        },
      ],
    },
  ],
};

export default guide;
