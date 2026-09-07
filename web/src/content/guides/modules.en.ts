// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

const guide: GuideContent = {
  slug: 'modules',
  meta: {
    title: 'The modules handbook - ItsBagelBot Guides',
    description:
      'Every ItsBagelBot module explained: chat alerts, auto shoutout, trigger words, loyalty points, channel-point rewards, timers, raffles, wager games, play queue, quotes, game stats, emote pyramids and AutoMod.',
    eyebrow: 'Guide 03',
    heading: 'The modules handbook',
    lead: 'Every module on one page: what it does, what it says, and which commands it brings to your chat.',
    minutes: '12 min read',
    card: {
      title: 'The modules handbook',
      description:
        'Every module, one page: alerts, timers, loyalty points, channel-point rewards, trigger words, the play queue, quotes, and the game-stat commands.',
      meta: '12 min · 6 steps',
      chips: ['alerts', 'timers', 'loyalty', 'game stats'],
    },
  },
  sections: [
    {
      id: 'how',
      heading: 'How modules work',
      note: 'One switch per feature. Click the tile to make its messages sound like you.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A module is a feature with an on/off switch. The
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">Modules page</a>
                shows them as tiles: the switch turns the feature on, clicking the tile opens its
                settings. Most modules speak in chat, and every line they say is a template you can
                rewrite, with the same brace variables as
                <a href="/guides/commands">custom commands</a> (plus a few extras per module, listed below).
                Every template comes with the same live chat rehearsal as commands, right under its
                editor: you watch the reply land in a mock chat before any viewer does.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesCategories',
          path: '/modules',
          caption: "Tiles grouped by category, a scrollspy rail on the left. Configure opens the module's page.",
          notes: [
            { n: 1, text: 'The quick switch. Off means the module is fully silent, settings kept for later.' },
            { n: 2, text: "Configure opens the module's own page, where its chat lines and options live." },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Two modules are on by default: <strong>Chat Alerts</strong> and <strong>AutoMod</strong>
                (which keeps itself off this grid). Everything else waits for you.
            </p>`,
        },
      ],
    },
    {
      id: 'welcome',
      heading: 'Welcoming people',
      note: 'Alerts, raid shoutouts, trigger words, and the time where you live.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Chat Alerts</h3>
            <p>
                Thanks new follows, subs, cheers and raids in chat, each with its own message and its
                own switch. Extra variables per alert: subs get <code>&#123;tier&#125;</code>, cheers
                get <code>&#123;bits&#125;</code>, raids get <code>&#123;viewers&#125;</code>.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Alerts with personality beat alerts with confetti.',
          lines: [
            { who: 'system', text: 'maya_live followed the channel' },
            { who: 'bot', text: 'Thanks for the follow, maya_live! Grab a seat 🥯' },
            { who: 'system', text: 'alex raided with 42 viewers' },
            { who: 'bot', text: 'alex brought 42 friends! Welcome, everyone!' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Auto Shoutout</h3>
            <p>
                When someone raids you, the bot plugs their channel without you alt-tabbing:
                <code>&#123;raider&#125;</code> is their display name,
                <code>&#123;raider.login&#125;</code> the URL-safe version for a twitch.tv link, and
                <code>&#123;viewers&#125;</code> the head-count. It can also fire Twitch's native
                /shoutout at the same time.
            </p>
            <h3>Trigger Words</h3>
            <p>
                Auto-replies without the "!": give the bot a phrase to watch for and the line to
                post when it shows up in ordinary chat. Each rule has its own little editor on the
                module page: the phrase, how it matches (whole word by default, so "hi" won't fire
                inside "this"; or contains, exact message, starts with), the response with the same
                rehearsal as commands, and its own on/off switch. The first matching rule wins, so
                one message gets at most one reply. Responses know <code>&#123;user&#125;</code>
                plus the dice-and-choices variables.
            </p>
            <h3>Local Time</h3>
            <p>
                Gives chat a <code>!time</code> command with your timezone and clock format, so nobody
                has to ask "what time is it for you?" ever again.
            </p>`,
        },
      ],
    },
    {
      id: 'economy',
      heading: 'Points, rewards & timers',
      note: 'Loyalty points for watching, channel-point rewards that do things, scheduled messages, and two wager games for the brave.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Loyalty Points</h3>
            <p>
                Viewers earn points for being around: watching, subbing, gifting, cheering. Name the
                points anything (bagels?), and chat checks their balance with <code>!points</code>.
                Mods can grant or set balances with <code>!points add</code> and
                <code>!points set</code>. This module also stores the
                <a href="/guides/counters">counters</a> behind <code>&#123;counter:…&#125;</code>, with
                mod tools under <code>!counter</code>.
            </p>
            <h3>Channel Points</h3>
            <p>
                Creates real Twitch channel-point rewards and binds each redemption to a bot action,
                like posting a templated line. You choose per reward whether redemptions are
                fulfilled, refunded, or left for a mod to judge. Templates know
                <code>&#123;user&#125;</code>, <code>&#123;input&#125;</code>,
                <code>&#123;reward&#125;</code>, <code>&#123;cost&#125;</code>,
                <code>&#123;counter&#125;</code> and <code>&#123;points&#125;</code>.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Party trick</b>
                Pair it with the <strong>Govee Lights</strong> module and a redemption can recolor the
                lights in your room. Chat picks sunset, your wall obeys.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Timers</h3>
            <p>
                Posts a message on a schedule while you're live: the Discord plug every 20 minutes, the
                hydration reminder every hour.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Heads up</b>
                Timer messages are posted exactly as written: brace variables do not run inside timers.
                Keep them for commands and module replies.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Gamble</h3>
            <p>
                Turns your points into a game: viewers stake their own balance with
                <code>!gamble 100</code>, or <code>!gamble half</code> / <code>!gamble all</code> for
                the fearless, and the bot rolls 1-100. Landing inside the win chance you set pays the
                stake back plus its match; anything else takes it. You also pick the bet limits and a
                per-viewer cooldown, and every payout and debit moves real loyalty points through the
                same ledger as <code>!points</code>. The win and lose lines are templates, with
                <code>&#123;roll&#125;</code>, <code>&#123;chance&#125;</code>,
                <code>&#123;amount&#125;</code> and <code>&#123;balance&#125;</code>.
            </p>
            <h3>Duels</h3>
            <p>
                Viewer-vs-viewer point duels, two ways. The pot: someone opens with
                <code>!duel 100</code>, everyone adds their own stake while the window is open, then
                the bot draws one winner weighted by stake and they take everything. The challenge:
                <code>!duel @maya_live 500</code> names an opponent who must type
                <code>!duel accept</code> before time runs out: equal stakes, a coin flip, winner
                takes both. Declines, cancellations and no-shows always refund every point.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Fair by design</b>
                Both games ride the Loyalty Points ledger: nobody can wager what they don't have, and
                the odds are exactly the number you set.`,
        },
      ],
    },
    {
      id: 'together',
      heading: 'Playing together',
      note: "A raffle chat can't argue with, a fair play queue, a quote book for chat's greatest hits, and a cheer squad for emote pyramids.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Raffle</h3>
            <p>
                A timed draw chat can't argue with. Viewers enter once with <code>!join</code>; the
                bot counts down out loud on a reminder cadence you set, then draws the winners
                itself when time runs out, uniformly at random, one entry per viewer, with a
                verifiable receipt kept. Winners confirm with <code>!claim</code>. You and your
                mods start one with <code>!raffle open</code>, close early with
                <code>!raffle draw</code>, or abort with <code>!raffle cancel</code>.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Opened, reminded, drawn, confirmed.',
          lines: [
            { who: 'mod', name: 'mod_sam', text: '!raffle open 10' },
            { who: 'bot', text: 'Raffle is LIVE! Type !join to enter. Drawing in 10 min!' },
            { who: 'bot', text: 'Raffle reminder: ~5 min left! 14 entered so far, type !join! Winners must !claim.' },
            { who: 'viewer', name: 'maya_live', text: '!join' },
            { who: 'bot', text: "@maya_live you're in! 15 entered so far. Good luck!" },
            { who: 'bot', text: '@crustycrumbs, @maya_live, congratulations! You won the raffle (2 winner(s) from 15)! Type !claim within 15 min to confirm your prize!' },
            { who: 'viewer', name: 'maya_live', text: '!claim' },
            { who: 'bot', text: '@maya_live your prize is confirmed, enjoy!' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Play Queue</h3>
            <p>
                "Can I play?" becomes self-service. Viewers join the line with <code>!join</code>,
                bail with <code>!leave</code>, and check the order with <code>!list</code>. You and
                your mods run <code>!queue open</code>, <code>!queue next</code>,
                <code>!queue close</code>. The conversational replies (join, leave, next up, queue
                opened and closed) are templates you can rewrite.
            </p>`,
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'The queue keeps itself honest.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!join' },
            { who: 'bot', text: 'maya_live joined the queue at spot #3.' },
            { who: 'mod', name: 'mod_sam', text: '!queue next' },
            { who: 'bot', text: "You're up, alex! 2 waiting behind you." },
          ],
        },
        {
          kind: 'prose',
          html: `
            <h3>Quotes</h3>
            <p>
                A quote book for the things chat refuses to let you forget. <code>!quote</code> serves
                a random one, <code>!quote 7</code> a specific one, and <code>!quote ferret</code> a
                random one containing that word. Saving new ones (<code>!addquote text</code>) and
                rewriting old ones (<code>!quote edit 7 text</code>) are each limited to whoever you
                allow. Mods can prune with <code>!quote remove</code>. The collection is browsable and
                editable from its dashboard page.
            </p>
            <h3>Emote Pyramids &amp; Streaks</h3>
            <p>
                A cheer squad for chat's art projects. The module watches chat stack the same emote
                into a pyramid (1-2-3 and back down) or hold a streak of single-emote messages, and
                fires a celebration line when one lands clean, nothing else posted in between.
                Fully automatic: no commands, no settings, just the switch.
            </p>`,
        },
      ],
    },
    {
      id: 'games',
      heading: 'Game stats in chat',
      note: 'Bedwars, MCSR Ranked, Fortnite, Clash Royale and Valorant, one command away.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Five integrations put game stats in chat so nobody alt-tabs. Link your account once on
                the module's page; every reply is a rewritable template with game-specific variables.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Module', 'Commands', 'What they answer'],
          rows: [
            [
              'Bedwars Stats',
              '<code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code>',
              'Hypixel Bedwars session and lifetime stats (wins, finals, beds, FKDR), plus sniper-network lookups.',
            ],
            [
              'MCSR Ranked',
              '<code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!race</code> <code>!pace</code> <code>!nethers</code> <code>!lastfort</code> <code>!pb</code>',
              "Minecraft speedrun Elo, rank and record, how this stream's session is going, the last match, head-to-head records, top-5 leaderboards, the weekly race, live PaceMan.gg split pace, and personal bests (daily/weekly/monthly/ranked).",
            ],
            [
              'Fortnite Stats',
              '<code>!fn</code> <code>!fn season</code> <code>!fn session</code> <code>!fn store</code>',
              "Lifetime, season and this-stream stats (wins, K/D, win rate), plus today's item shop.",
            ],
            [
              'Clash Royale Stats',
              '<code>!cr</code> <code>!cr decks</code> <code>!cr ranked</code> <code>!cr road</code>',
              'Lifetime profile (level, win/loss, win rate), the current battle deck with its average elixir, Path of Legends standing, and trophy-road record, by player tag.',
            ],
            [
              'Valorant Stats',
              '<code>!val</code> <code>!val matches</code> <code>!val account</code> <code>!val lb</code> <code>!val shop</code>',
              "Competitive standing (tier, RR, last game's change, peak tier), recent ranked games as agent K/D/A lines, who a Riot ID resolves to with account level, regional top-10 leaderboards on PC or console, and today's skin rotation with VP prices and the reset countdown. Squashed spellings like <code>!valrank</code> work too; a shard or ladder word anywhere in the line scopes that one lookup.",
            ],
          ],
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'Viewers can also look up other players by adding a name.',
          lines: [
            { who: 'viewer', name: 'maya_live', text: '!elo' },
            { who: 'bot', text: 'your_channel: 1650 elo · rank #12 · 40W 20L this season' },
          ],
        },
      ],
    },
    {
      id: 'safety',
      heading: 'Moderation & built-ins',
      note: "AutoMod's four levels, and the four commands that come with the bot.",
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>AutoMod</h3>
            <p>
                The layered moderation from the homepage. It ships on its recommended middle setting
                and starts protecting the moment the bot joins: harassment, sexual content,
                profanity, caps and symbol spam, and link spam are screened, and the safety floor
                (slurs, scam links) can never be turned off. There is no tile or settings page for
                it in the dashboard yet; the finer controls are on their way.
            </p>
            <h3>The four built-ins</h3>
            <p>
                On the <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands page</a>,
                above your own commands, live four that ship with the bot:
            </p>
            <ul>
                <li><code>!followage</code>: how long someone has followed you.</li>
                <li><code>!accountage</code>: how old a Twitch account is.</li>
                <li><code>!uptime</code>: how long your current stream has been live.</li>
                <li><code>!clip</code>: creates a clip of the last moments and posts the link (live only). Its reply is a template you can rewrite, with <code>&#123;clip&#125;</code>, <code>&#123;user&#125;</code> and <code>&#123;target&#125;</code>.</li>
            </ul>
            <p>
                They can be toggled like any module, but not renamed or deleted. That's the whole
                handbook: switch things on as your community grows into them.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Build something</b>
                Ready to write commands of your own? The
                <a href="/command-builder">command builder</a> does the syntax for you.`,
        },
      ],
    },
  ],
};

export default guide;
