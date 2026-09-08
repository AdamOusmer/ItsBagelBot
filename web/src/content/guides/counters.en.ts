// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "counters" guide in en. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/counters.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'basics.b0.html': `
            <p>
                A counter is a named number the bot remembers between streams. Write
                <code>&#123;counter:deaths&#125;</code> in a command's response, and every time the
                command runs, the bot adds 1 to a counter called "deaths" and shows the new total
                right there in its reply. No spreadsheet, no manual tally, just a token.
            </p>`,
    'basics.b1.caption': 'Two runs of the same !death command, one counter remembering both.',
    'basics.b1.lines.0.name': 'alex',
    'basics.b1.lines.0.text': '!death',
    'basics.b1.lines.1.text': 'your_channel has died 47 times.',
    'basics.b1.lines.2.name': 'maya_live',
    'basics.b1.lines.2.text': '!death',
    'basics.b1.lines.3.text': 'your_channel has died 48 times.',
    'basics.b1.title': '#your_channel',
    'basics.b2.html': `
                <b>Counters need Loyalty Points</b>
                Counters are stored by the <a href="/guides/modules">Loyalty Points module</a>. Switch
                it on first, or <code>&#123;counter:…&#125;</code> stays as literal text in chat.`,
    'basics.heading': 'Counters: numbers that remember',
    'basics.note': 'One token, {counter:name}, adds 1 and shows the total. The count survives every stream.',
    'chat.b0.html': `
            <p>
                You and your moderators can create and manage counters without leaving chat, all
                under <code>!counter</code>:
            </p>`,
    'chat.b1.head.0': 'Command',
    'chat.b1.head.1': 'What it does',
    'chat.b1.rows.0.0': '<code>!counter create &lt;name&gt; [scope]</code>',
    'chat.b1.rows.0.1': 'Creates a counter. Scope word is <code>user</code>, <code>command</code> or <code>user+command</code>; leave it out for the whole channel.',
    'chat.b1.rows.1.0': '<code>!counter add &lt;name&gt; [amount]</code>',
    'chat.b1.rows.1.1': 'Adds 1, or the amount you give. Negative numbers subtract.',
    'chat.b1.rows.2.0': '<code>!counter set &lt;name&gt; &lt;value&gt;</code>',
    'chat.b1.rows.2.1': 'Sets the total outright.',
    'chat.b1.rows.3.0': '<code>!counter reset &lt;name&gt;</code>',
    'chat.b1.rows.3.1': 'Clears it back to zero. For per-user or per-command counters, this wipes every stored bucket.',
    'chat.b1.rows.4.0': '<code>!counter delete &lt;name&gt;</code>',
    'chat.b1.rows.4.1': 'Removes the counter entirely.',
    'chat.b1.rows.5.0': '<code>!counter list</code>',
    'chat.b1.rows.5.1': 'Lists every counter on the channel and its scope.',
    'chat.b1.rows.6.0': '<code>!counter &lt;name&gt;</code>',
    'chat.b1.rows.6.1': 'Shows the current value, same as reading it in chat.',
    'chat.b2.caption': 'Creating and bumping a counter, start to finish, without touching the dashboard.',
    'chat.b2.lines.0.name': 'mod_sam',
    'chat.b2.lines.0.text': '!counter create deaths',
    'chat.b2.lines.1.text': 'Counter deaths created (channel).',
    'chat.b2.lines.2.name': 'mod_sam',
    'chat.b2.lines.2.text': '!counter add deaths 5',
    'chat.b2.lines.3.text': 'Counter deaths is now 5.',
    'chat.b2.title': '#your_channel',
    'chat.b3.html': `
                <b>Command scope, from chat</b>
                <code>!counter create hydrations command</code> makes a pooled per-command counter in
                one line, ready to drop into a command's response, or bind to a channel-point reward,
                as <code>&#123;counter:hydrations&#125;</code>.`,
    'chat.b4.labels.addOne': '!counter add deaths',
    'chat.b4.labels.heading': 'Try it: bump the counter',
    'chat.b4.labels.modName': 'mod_sam',
    'chat.b4.labels.modReplyTemplate': 'Counter deaths is now {value}.',
    'chat.b4.labels.setTen': '!counter set deaths 10',
    'chat.b4.labels.subOne': '!counter add deaths -1',
    'chat.b4.labels.viewerCommand': '!death',
    'chat.b4.labels.viewerName': 'crust',
    'chat.b4.labels.viewerReplyTemplate': 'Oh no, {value} deaths so far.',
    'chat.b4.labels.windowTitle': '#your_channel',
    'chat.b4.props.counterName': 'deaths',
    'chat.heading': 'Managing counters from chat',
    'chat.note': 'Create, bump, set, reset, delete, or just ask, all without leaving chat.',
    'dashboard.b0.html': `
            <p>
                The <a href="https://dashboard.itsbagelbot.com/counters" target="_blank" rel="noopener noreferrer">Counters</a>
                page lists every counter on your channel and lets you create, adjust, and delete them
                by clicking instead of typing.
            </p>`,
    'dashboard.b1.caption': 'New counter panel: name it, pick a scope, done. Existing rows get +/- steppers for whole-channel counters, or a values table for the others.',
    'dashboard.b1.notes.0.text': "Name: the same string you'd write inside {counter:…}.",
    'dashboard.b1.notes.1.text': 'Counts: the scope. Fixed once you create it.',
    'dashboard.b1.notes.2.text': 'A plain-language reminder of where counters get bumped from.',
    'dashboard.b2.html': `
            <h3>Binding a counter to a channel-point reward</h3>
            <p>
                Every <a href="/guides/modules">Channel Points</a> reward can optionally keep a
                counter of its own, right from the reward's editor: type a counter name (new or
                existing), pick its scope, and every redemption bumps it. Use
                <code>&#123;counter&#125;</code> in the reward's chat reply to show the new total.
            </p>`,
    'dashboard.b3.caption': "The reward editor's counter block: a toggle, a name, and the same four scopes.",
    'dashboard.b3.notes.0.text': "Off by default, most rewards don't need one.",
    'dashboard.b3.notes.1.text': 'Per user + reward is usually the right call: each viewer builds their own redemption count for this reward.',
    'dashboard.b4.html': `
                <b>Try it in the builder</b>
                The <a href="/command-builder">command builder</a> can walk you through naming a
                counter and picking its scope, then hands you the finished
                <code>&#123;counter:…&#125;</code> token.
                <a href="/command-builder">Open the command builder</a>`,
    'dashboard.heading': 'The dashboard, and channel-point rewards',
    'dashboard.note': 'The Counters page for hands-on control, and how a reward binds to one.',
    'meta.card.chips.0': '{counter:…}',
    'meta.card.chips.1': 'scopes',
    'meta.card.chips.2': '!counter',
    'meta.card.chips.3': 'channel points',
    'meta.card.description': 'Numbers the bot remembers forever: the {counter:name} token, the four scopes (channel, per user, per command, per user + command), and binding one to a channel-point reward.',
    'meta.card.meta': '8 min · 4 steps',
    'meta.card.title': 'Counters',
    'meta.description': 'How ItsBagelBot counters work: the {counter:name} token, the four scopes (whole channel, per user, per command, per user + command), managing them with !counter, and binding one to a channel-point reward.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Counters',
    'meta.lead': "Track anything that happens more than once: deaths, hugs, redemptions. Choose how it's counted once, and the bot remembers forever.",
    'meta.minutes': '8 min read',
    'meta.title': 'Counters - ItsBagelBot Guides',
    'scopes.b0.html': `
            <p>
                Every counter has exactly one <strong>scope</strong>, chosen the moment it's created,
                and it stays that way for the counter's whole life. Scope answers one question: whose
                number is this?
            </p>`,
    'scopes.b1.head.0': 'Scope',
    'scopes.b1.head.1': 'Chat word',
    'scopes.b1.head.2': "What's counted",
    'scopes.b1.rows.0.0': 'Whole channel',
    'scopes.b1.rows.0.1': '<em>(default)</em>',
    'scopes.b1.rows.0.2': 'One shared total, everyone adds to the same number.',
    'scopes.b1.rows.1.0': 'Per user',
    'scopes.b1.rows.1.1': '<code>user</code>',
    'scopes.b1.rows.1.2': 'Each viewer gets their own number.',
    'scopes.b1.rows.2.0': 'Per command or reward',
    'scopes.b1.rows.2.1': '<code>command</code>',
    'scopes.b1.rows.2.2': 'One shared total for a single command or reward, every viewer pooled together.',
    'scopes.b1.rows.3.0': 'Per user + command or reward',
    'scopes.b1.rows.3.1': '<code>user+command</code>',
    'scopes.b1.rows.3.2': 'Each viewer gets a separate number for each command or reward.',
    'scopes.b10.html': `
                <b>Locked once created</b>
                A counter's scope can't be changed afterward. Picked the wrong one? Delete it
                (<code>!counter delete name</code>, or the dashboard) and create it again; the old
                values don't carry over.`,
    'scopes.b2.html': `
            <h3>Whole channel</h3>
            <p>
                Everyone bumps the same shared number. Perfect for a tally that belongs to the stream
                itself, not to any one viewer, like a running death count.
            </p>`,
    'scopes.b3.caption': "Doesn't matter who types !death, the channel's total keeps climbing.",
    'scopes.b3.lines.0.name': 'alex',
    'scopes.b3.lines.0.text': '!death',
    'scopes.b3.lines.1.text': 'your_channel has died 47 times.',
    'scopes.b3.lines.2.name': 'sam',
    'scopes.b3.lines.2.text': '!death',
    'scopes.b3.lines.3.text': 'your_channel has died 48 times.',
    'scopes.b3.title': '#your_channel',
    'scopes.b4.html': `
            <h3>Per user</h3>
            <p>
                Each viewer gets a private number, untouched by anyone else's. Good for personal
                streaks: how many hugs someone has given, how many times they've won a game against
                you.
            </p>`,
    'scopes.b5.caption': 'Same command, two viewers, two totally separate counts.',
    'scopes.b5.lines.0.name': 'maya_live',
    'scopes.b5.lines.0.text': '!hug',
    'scopes.b5.lines.1.text': 'maya_live has given 12 hugs.',
    'scopes.b5.lines.2.name': 'alex',
    'scopes.b5.lines.2.text': '!hug',
    'scopes.b5.lines.3.text': 'alex has given 1 hug.',
    'scopes.b5.title': '#your_channel',
    'scopes.b6.html': `
            <h3>Per command or reward</h3>
            <p>
                The newest scope: one pooled total for a single command or channel-point reward,
                with every viewer adding to the same number. Ideal for "how many times has this
                specific reward been redeemed, total?"
            </p>`,
    'scopes.b7.caption': "Two different viewers, the reward's own running total either way.",
    'scopes.b7.lines.0.text': 'channel points · maya_live redeemed Hydrate!',
    'scopes.b7.lines.1.text': 'Hydrate! has been redeemed 301 times.',
    'scopes.b7.lines.2.text': 'channel points · alex redeemed Hydrate!',
    'scopes.b7.lines.3.text': 'Hydrate! has been redeemed 302 times.',
    'scopes.b7.title': '#your_channel',
    'scopes.b8.html': `
            <h3>Per user + command or reward</h3>
            <p>
                Combines both: every viewer gets a separate number for each command or reward that
                shares this counter. Bind one counter to two commands and each viewer ends up with
                one bucket per command, not one shared bucket for both.
            </p>`,
    'scopes.b9.caption': "alex's count for !hug and their count for !highfive don't mix, even though both use the same counter.",
    'scopes.b9.lines.0.name': 'alex',
    'scopes.b9.lines.0.text': '!hug',
    'scopes.b9.lines.1.text': 'alex has hugged 3 times.',
    'scopes.b9.lines.2.name': 'alex',
    'scopes.b9.lines.2.text': '!highfive',
    'scopes.b9.lines.3.text': 'alex has high-fived 1 time.',
    'scopes.b9.title': '#your_channel',
    'scopes.heading': 'Four ways to count',
    'scopes.note': 'Whole channel, per user, per command, or both. Pick one when you create the counter; it sticks.',
};

export default strings;
