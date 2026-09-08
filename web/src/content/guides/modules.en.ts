// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from '../../lib/guides/types';

// The catalogue behind the ModuleCatalog widget. Names and taglines are the
// dashboard's own strings, so they stay English in every locale; categories,
// plan, start state and the "needs" line are ours to translate.
const categories = ['Moderation', 'Chat', 'Channel', 'Points', 'Play', 'Gear', 'Stats'];

const modules = [
  {
    name: 'AutoMod',
    tagline: 'Catch scams, IP-grabbers and raid spam before your mods do.',
    cat: 'Moderation',
    plan: 'premium',
    start: 'On by default',
  },
  {
    name: 'Trigger Words',
    tagline: 'Auto-reply when a word shows up in chat, no "!" needed.',
    cat: 'Chat',
    start: 'Off by default',
  },
  {
    name: 'Local Time',
    tagline: 'Viewers ask what time it is for you with !time.',
    cat: 'Chat',
    start: 'Off by default',
    needs: 'A timezone, picked on the module page.',
    commands: '!time',
  },
  {
    name: 'Quotes',
    tagline: 'Save the best things said on stream and replay them in chat.',
    cat: 'Chat',
    start: 'Off by default',
    commands: '!quote, !quotes, !quote <n>, !quote <word>, !addquote, !quoteadd, !quote edit <n> <text>, !quote remove <n>',
  },
  {
    name: 'Timers',
    tagline: 'Post repeating chat messages on a schedule while you are live.',
    cat: 'Chat',
    start: 'Off by default',
    needs: 'A live stream. Timers stay quiet when you are offline.',
  },
  {
    name: 'Emote Pyramids & Streaks',
    tagline: 'Celebrate chat-built emote pyramids and emote streaks.',
    cat: 'Chat',
    start: 'Off by default',
  },
  {
    name: 'Chat Alerts',
    tagline: 'Announce follows, subs, cheers, raids and ad breaks in chat.',
    cat: 'Channel',
    start: 'On by default',
    needs: 'The Twitch permissions you granted at login.',
  },
  {
    name: 'Auto Shoutout',
    tagline: 'Welcome incoming raids with an automatic shoutout.',
    cat: 'Channel',
    start: 'Off by default',
    needs: 'Mod permission, if you also want the native Twitch /shoutout.',
  },
  {
    name: 'Channel Points',
    tagline: 'Turn channel-point redemptions into bot actions.',
    cat: 'Channel',
    start: 'Off by default',
    needs: 'Affiliate or partner. The bot creates the rewards itself.',
  },
  {
    name: 'Stream Management',
    tagline: 'Set the live title, category and tags, run ads, and drop markers from chat.',
    cat: 'Channel',
    start: 'Always on',
    commands: '!title, !settitle, !game, !setgame, !tags, !settags, !commercial, !ad, !marker, !cmd, !cmds, !command, !commands',
  },
  {
    name: 'Loyalty Points',
    tagline: 'Viewers earn channel currency for subs, cheers and watch time.',
    cat: 'Points',
    start: 'Off by default',
    commands: '!points, !points give @user 50, !leaderboard, !points set @user 500, !points add @user 100, !points remove @user 100',
  },
  {
    name: 'Counters',
    tagline: 'Track wins, deaths, hugs, redeems, anything your chat can count.',
    cat: 'Points',
    start: 'Always on',
    commands: '!counter',
  },
  {
    name: 'Gamble',
    tagline: 'Let viewers wager their points on a roll with !gamble.',
    cat: 'Points',
    start: 'Off by default',
    needs: 'Loyalty Points on. Gamble is a row on the Loyalty page.',
    commands: '!gamble <amount>, !gamble half, !gamble all',
  },
  {
    name: 'Duels',
    tagline: 'Viewer-vs-viewer point duels: pot free-for-alls and 1v1 challenges.',
    cat: 'Points',
    start: 'Off by default',
    needs: 'Loyalty Points on. Duels is a row on the Loyalty page.',
    commands: '!duel, !duel <stake>, !duel <user> <stake>, !duel accept, !duel decline, !duel cancel',
  },
  {
    name: 'Play Queue',
    tagline: 'Let viewers line up to play with you, first come first served.',
    cat: 'Play',
    start: 'Off by default',
    commands: '!join, !leave, !list, !queuelist, !queue, !queue open, !queue close, !queue next, !queue remove <user>, !queue clear',
  },
  {
    name: 'Raffle',
    tagline: 'Timed random draws your chat enters with !join.',
    cat: 'Play',
    start: 'Off by default',
    needs: 'Raffle owns !join when Play Queue is on as well.',
    commands: '!join, !claim, !winner, !raffle, !raffle open [minutes] [winners] [remind], !raffle draw [winners], !raffle close, !raffle cancel',
  },
  {
    name: 'Song Requests',
    tagline: '!sr and a channel-points reward that queue songs from Spotify.',
    cat: 'Gear',
    start: 'Off by default',
    needs: 'Your own Spotify app, plus a Spotify login.',
    commands: '!sr <song or link>, !remove, !srlist, !songlist, !current, !song, !nowplaying, !np, !skip, !next, !clear',
  },
  {
    name: 'Govee Lights',
    tagline: 'Let viewers recolour your Govee lights with channel points.',
    cat: 'Gear',
    start: 'Off by default',
    needs: 'A Govee API key and a channel-point reward. Works while you are live.',
  },
  {
    name: 'Discord',
    tagline: 'One bot on Twitch and Discord. Go-live, clips, welcomes, tickets, and voice.',
    cat: 'Gear',
    plan: 'premium',
    start: 'Off by default',
    needs: 'A Discord server connected, or one created from the template.',
  },
  {
    name: 'Bedwars Stats',
    tagline: 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
    cat: 'Stats',
    start: 'Off by default',
    needs: 'Your Minecraft username.',
    commands: '!daily, !bwdaily, !weekly, !bwweekly, !monthly, !bwmonthly, !bwstats, !bedwars, !sniper, !urchin, !tag, !tags, !bwtags, !tagdescription',
  },
  {
    name: 'MCSR Ranked',
    tagline: 'Ranked elo and per-stream session stats for MCSR runners.',
    cat: 'Stats',
    start: 'Off by default',
    needs: 'Your Minecraft username, an MCSR Ranked account and a PaceMan account.',
    commands: '!elo [player], !lastmatch, !record <a> <b>, !matchrecord, !lb, !leaderboard, !rankedlb, !session, !mcsrsession, !race, !weeklyrace, !pb, !personalbest daily, !pace, !nethers, !lastfort',
  },
  {
    name: 'Fortnite Stats',
    tagline: 'Fortnite BR stats and the daily item shop in chat.',
    cat: 'Stats',
    start: 'Off by default',
    needs: 'Your Epic display name.',
    commands: '!fn [player], !fnstats, !fortnitestats, !fnseason, !fnsession, !fnstore, !itemshop, !fnshop',
  },
  {
    name: 'Clash Royale Stats',
    tagline: 'Clash Royale profiles, decks and Path of Legends standing in chat.',
    cat: 'Stats',
    start: 'Off by default',
    needs: 'Your Supercell player tag, like #P2LQ0GR.',
    commands: '!cr [tag], !crstats, !clashroyale, !crdecks, !crdeck, !crranked, !crpol, !crroad, !crtrophy',
  },
  {
    name: 'Valorant Stats',
    tagline: 'Valorant ranks, match history, leaderboards and the daily shop rotation in chat.',
    cat: 'Stats',
    start: 'Off by default',
    needs: 'Your Riot ID, like Frosty#EUW1, and your region.',
    commands: '!val [RiotID], !valrank, !valmatches, !valhistory, !valaccount, !valwho, !vallb, !valleaderboard, !valshop, !valrotation',
  },
];

const guide: GuideContent = {
  slug: 'modules',
  meta: {
    title: 'Modules - ItsBagelBot Guides',
    description:
      'The ItsBagelBot modules page explained: the seven categories, the tile shapes, how a module is configured, points and games, game stats in chat, and the nine built-in commands.',
    eyebrow: 'Guide',
    heading: 'Modules',
    lead: 'Every feature on your channel is a tile with a switch. Here is what each one does, what it needs first, and what the odd tiles mean.',
    minutes: '10 min read',
    card: {
      title: 'Modules',
      description:
        'The 24 tiles on your Modules page: what each one does, what it needs before it works, and which commands it brings to chat.',
      meta: '10 min · 7 sections',
      chips: ['moderation', 'chat', 'points', 'stats'],
    },
  },
  sections: [
    {
      id: 'page',
      heading: 'The modules page',
      note: 'Seven categories, one switch per tile, six tile shapes.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                A module is one feature of the bot with its own switch. The
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">Modules page</a>
                lists them as tiles, grouped by the Categories rail on the left: Moderation, Chat,
                Channel, Points, Play, Gear, Stats. The line under the title counts what is running,
                and the search box matches a module name, what it does, or a chat command you half
                remember.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModulesGrid',
          path: '/modules',
          caption: 'The Modules page: the Categories rail, tiles with a Configure button, and the switch.',
          notes: [
            { n: 1, text: 'The Categories rail. Seven groups, in this order, and clicking one scrolls the grid to it.' },
            { n: 2, text: "A tile is one module: its name, its category, and the one line the dashboard uses to describe it." },
            { n: 3, text: "Configure opens the module's own page, where its settings and its chat lines live." },
            { n: 4, text: 'The switch. Off means the module says nothing, and every setting stays where you left it.' },
            { n: 5, text: 'AutoMod carries a "Beta · Premium" chip. On a free channel the tile is locked.' },
            { n: 6, text: 'Counters has no switch. It is always on, and so is Stream Management.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Most tiles behave the same way: flip the switch, click Configure, done. Five tiles
                behave differently, and knowing which is which saves you hunting for a switch that
                was never there.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Tile shape', 'What you see', 'Which modules'],
          rows: [
            [
              'Ordinary',
              'A switch and a Configure button. The switch turns the feature on for your channel.',
              'Timers, Quotes, Raffle, and most of the grid.',
            ],
            [
              'Hidden',
              'The module runs inside the bot and never reaches the grid, because it has nothing for you to set.',
              'The internal plumbing behind commands.',
            ],
            [
              'Section',
              'The module skips the grid and gets a page of its own in the dashboard.',
              'Discord.',
            ],
            [
              'Nested',
              'A row on the parent module\'s page, with no switch of its own. The row says: "This game spends <code>&#123;parent&#125;</code>. Turn it on from there. It cannot run on its own."',
              'Gamble and Duels, on the Loyalty Points page.',
            ],
            [
              'Always on',
              'A tile with a Configure button, and the switch is missing on purpose. The feature runs whatever you do.',
              'Counters, Stream Management.',
            ],
            [
              'Beta',
              'A locked tile with a "Beta · Premium" chip, and the settings underneath once you have Premium.',
              'AutoMod, Discord.',
            ],
          ],
          caption: 'Six tile shapes, five of which surprise people at least once.',
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                Two modules are premium while they are in beta: AutoMod and Discord. Everything else
                on this page works on the free plan, for as long as you like.`,
        },
      ],
    },
    {
      id: 'catalog',
      heading: 'Every module',
      note: 'The whole catalogue, filtered by category or by a command.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Here is the full list, the same one the dashboard draws. Each card carries the
                module's own one-line description, its category, whether it starts on or off, what
                you have to set up first, and the chat commands it adds. Pick a category chip, or
                type into the filter: it matches names, descriptions and commands, so
                <code>!sr</code> finds Song Requests and "elo" finds MCSR Ranked.
            </p>`,
        },
        {
          kind: 'widget',
          name: 'ModuleCatalog',
          labels: {
            all: 'All',
            searchLabel: 'Filter modules',
            searchPlaceholder: 'Name, command, or what it does',
            countAll: '{n} modules',
            countCat: '{n} in {cat}',
            countMatch: '{shown} of {total}',
            commands: 'Commands',
            needs: 'Needs',
            free: 'Free',
            premium: 'Premium beta',
            empty: 'Nothing matches that. Try a command like !sr, or a game name.',
          },
          props: { categories, modules },
        },
        {
          kind: 'callout',
          tone: 'tip',
          html: `
                <b>Tip</b>
                The same search sits at the top of the dashboard page. If a viewer asks for something
                and you cannot remember which module owns it, type the command there.`,
        },
      ],
    },
    {
      id: 'configure',
      heading: 'Configure a module',
      note: 'Chat Alerts, its six replies, and the rehearsal under the editor.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Configure opens the module's own page. It always has the same three parts: the
                module status at the top, the settings, and the replies it posts in chat. Chat
                Alerts is a good one to learn on, because it has six replies with six switches:
                follow, subscribe, gift sub, cheer, raid, and ad break.
            </p>`,
        },
        {
          kind: 'dash',
          screen: 'ModuleConfigure',
          path: '/modules/alerts',
          caption: 'Chat Alerts: module status, the six replies, and the editor docked on the right.',
          notes: [
            { n: 1, text: 'Module status. One switch for the whole module, and turning it off keeps every setting.' },
            { n: 2, text: 'Each reply is a row you can open, with a switch of its own. Chat Alerts has six.' },
            { n: 3, text: 'The ad break alert ships off. Turn it on if you want chat warned before the ads roll.' },
            { n: 4, text: 'The message. Braces are tokens the bot fills in: {user} here, {bits} on the cheer alert.' },
            { n: 5, text: 'The rehearsal, same as the command editor. You watch the line land before a viewer does.' },
          ],
        },
        {
          kind: 'prose',
          html: `
            <p>
                Every reply is a template. <code>&#123;user&#125;</code> is the viewer who set it off,
                and each alert adds its own: <code>&#123;tier&#125;</code> on subs,
                <code>&#123;count&#125;</code> on gift subs, <code>&#123;bits&#125;</code> on cheers,
                <code>&#123;viewers&#125;</code> on raids, <code>&#123;duration&#125;</code> on ad
                breaks. The editor lists the tokens that reply accepts, and the catalogue above tells
                you which modules have replies to rewrite. Leave a message blank and the bot uses the
                default line.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                A follow alert fires at most once per viewer every three days, so someone unfollowing
                and refollowing cannot spam your chat.`,
        },
      ],
    },
    {
      id: 'points',
      heading: 'Points, gamble and duels',
      note: 'Loyalty is the parent. Gamble and Duels are rows on its page.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                <strong>Loyalty Points</strong> gives your channel a currency. You name it (bagels,
                crumbs, whatever chat will say out loud) and set what earns it: a sub, a resub, a
                gift sub, a cheer, and watch time counted every 5 minutes. Viewers check their
                balance with <code>!points</code>, hand some over with
                <code>!points give @maya_live 50</code>, and compare with <code>!leaderboard</code>.
                Mods correct the ledger with <code>!points set</code>, <code>!points add</code> and
                <code>!points remove</code>.
            </p>
            <p>
                Two games spend that currency, and both live as rows on the Loyalty Points page
                rather than as tiles of their own.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Game', 'Defaults', 'Commands'],
          rows: [
            [
              'Gamble',
              'Win chance 50%, adjustable from 1 to 99. Minimum bet 1, maximum 1000. Cooldown 10 s per viewer.',
              '<code>!gamble 100</code>, <code>!gamble half</code>, <code>!gamble all</code>',
            ],
            [
              'Duels',
              'Stakes from 1 to 1000. A pot stays open 60 s. A named challenge waits 120 s for an answer.',
              '<code>!duel</code>, <code>!duel 500</code>, <code>!duel @ferret_king 500</code>, <code>!duel accept</code>',
            ],
          ],
          caption: 'The numbers the bot ships with. Every one of them is yours to change.',
        },
        {
          kind: 'chat',
          title: '#your_channel',
          caption: 'A roll that pays, a roll that does not, and a duel that ends badly for one of them.',
          lines: [
            { who: 'viewer', name: 'sesame_sam', text: '!gamble 100' },
            { who: 'bot', text: '@sesame_sam rolled 37 (needed 50 or less) and won 100 bagels, now at 480!' },
            { who: 'viewer', name: 'ferret_king', text: '!gamble 250' },
            { who: 'bot', text: '@ferret_king rolled 88 (needed 50 or less) and lost 250 bagels. Now at 90.' },
            { who: 'viewer', name: 'maya_live', text: '!duel @ferret_king 500' },
            { who: 'bot', text: '@maya_live challenges @ferret_king for 500 bagels! @ferret_king, type !duel accept within 120s. Winner takes 1000!' },
            { who: 'viewer', name: 'ferret_king', text: '!duel accept' },
            { who: 'bot', text: 'The blades fall: @maya_live defeats @ferret_king and takes 1000 bagels!' },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Watch out</b>
                Looking for Gamble or Duels on the modules grid is a wasted trip. Turn on Loyalty
                Points, open its page, and switch the games on from the rows there.`,
        },
      ],
    },
    {
      id: 'games',
      heading: 'Game stats in chat',
      note: 'Five modules, one account field each, one command your chat will wear out.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Five modules answer "what rank are you?" so you do not have to. Each one asks for a
                single account field on its page, then every reply is a template with that game's
                own tokens. Aliases are generous: the short spelling and the long one both work.
            </p>`,
        },
        {
          kind: 'cards',
          columns: 3,
          items: [
            {
              title: 'Bedwars Stats',
              html: `
                <p>Set your Minecraft username. Seven reply templates, cooldown per command.</p>
                <p><code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code></p>
                <p>sesame_sam today: 12W 4L · 210 finals · 34 beds · 3.1 FKDR</p>`,
              chips: ['Hypixel'],
            },
            {
              title: 'MCSR Ranked',
              html: `
                <p>Set your Minecraft username. Needs an MCSR Ranked account and a PaceMan account. Per-command toggles.</p>
                <p><code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!pace</code> <code>!pb</code></p>
                <p>sesame_sam: 1650 elo · rank #12 · 40W 20L this season</p>`,
              chips: ['Minecraft'],
            },
            {
              title: 'Fortnite Stats',
              html: `
                <p>Set your Epic display name. Account type defaults to Epic.</p>
                <p><code>!fn</code> <code>!fnstats</code> <code>!fnseason</code> <code>!fnsession</code> <code>!fnstore</code></p>
                <p>Item Shop 2026-09-07: Renegade Raider, Aerial Assault Trooper, Take the L</p>`,
              chips: ['Epic'],
            },
            {
              title: 'Clash Royale Stats',
              html: `
                <p>Set your Supercell player tag, the one that looks like #P2LQ0GR.</p>
                <p><code>!cr</code> <code>!crstats</code> <code>!crdecks</code> <code>!crranked</code> <code>!crroad</code></p>
                <p>sesame_sam · level 42 · 5120W/4380L · 54% WR · 1180 three-crowns · Crust Clan</p>`,
              chips: ['Supercell'],
            },
            {
              title: 'Valorant Stats',
              html: `
                <p>Set your Riot ID and region. Region defaults to eu, platform to pc.</p>
                <p><code>!val</code> <code>!valrank</code> <code>!valmatches</code> <code>!vallb</code> <code>!valshop</code></p>
                <p>Frosty#EUW1 · Immortal 2 · 143 RR (+21) · peak Immortal 3</p>`,
              chips: ['Riot'],
            },
          ],
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Watch out</b>
                Two spellings people get wrong. The Fortnite season command is
                <code>!fnseason</code>, written as one word. And the MCSR session resets the moment
                your stream goes live, so <code>!session</code> answers for tonight, not for the week.`,
        },
      ],
    },
    {
      id: 'gear',
      heading: 'Song requests, lights, Discord',
      note: 'The three modules that reach outside Twitch, and what each one asks for first.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <h3>Song Requests</h3>
            <p>
                Chat queues music into your own Spotify with <code>!sr a song name</code> or a
                Spotify link. You decide which permission tier may request. Everyone gets
                <code>!current</code> (also <code>!song</code>, <code>!nowplaying</code>,
                <code>!np</code>) and <code>!srlist</code>; mods get <code>!skip</code> and
                <code>!clear</code>. Setup asks for two things: your own Spotify app, and a Spotify
                login. A channel-point reward that queues a song is optional, and set up on the same
                page.
            </p>`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Govee Lights</h3>
            <p>
                Viewers spend channel points to recolour the Govee lights in your room. You paste a
                Govee API key, pick the device, and bind a reward. The key is encrypted and never
                shown back to you, not even to the dashboard. To get one: Govee Home app &gt;
                Profile &gt; gear &gt; "Apply for API Key".
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'warn',
          html: `
                <b>Watch out</b>
                Lights only answer while you are live. A redemption that lands off stream is refunded
                automatically, so nobody pays for a dark room.`,
        },
        {
          kind: 'prose',
          html: `
            <h3>Discord</h3>
            <p>
                The same bot on both sides: go-live posts, clips, welcomes, tickets and voice rooms.
                Discord skips the modules grid and says nothing in Twitch chat; it gets a page of
                its own in the dashboard. Connect a server you already run, or let the bot
                build one from the template. It is premium while the beta lasts, and what you
                configure during the beta keeps working afterwards.
            </p>`,
        },
      ],
    },
    {
      id: 'builtins',
      heading: 'Always on',
      note: 'Nine commands ship with the bot, plus a handful of small ones.',
      blocks: [
        {
          kind: 'prose',
          html: `
            <p>
                Above your own commands, the
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands page</a>
                lists nine built-in commands. They arrive with the bot, they can be switched off, and
                they cannot be renamed or deleted.
            </p>`,
        },
        {
          kind: 'table',
          head: ['Command', 'Who can run it', 'What it answers'],
          rows: [
            ['<code>!accountage</code>', 'Everyone', 'How old a Twitch account is.'],
            ['<code>!followage</code>', 'Everyone', 'How long someone has followed you.'],
            ['<code>!uptime</code>', 'Everyone', 'How long the current stream has been live.'],
            [
              '<code>!clip</code>',
              'Everyone, live only',
              'Clips the last moments and posts the link. <code>!clip30</code> makes a 30 second clip.',
            ],
            ['<code>!title</code> <code>!settitle</code>', 'Lead mod', 'Reads the stream title, or sets a new one.'],
            ['<code>!game</code> <code>!setgame</code>', 'Lead mod', 'Reads the category, or sets a new one.'],
            ['<code>!tags</code> <code>!settags</code>', 'Lead mod', 'Reads the stream tags, or replaces them.'],
            ['<code>!commercial</code> <code>!ad</code>', 'Lead mod, live only', 'Starts an ad break.'],
            ['<code>!marker</code>', 'Lead mod, live only', 'Drops a marker you can find later in the VOD.'],
          ],
          caption: 'The nine built-ins, and who chat lets run them.',
        },
        {
          kind: 'prose',
          html: `
            <p>
                A few more answer without appearing anywhere: <code>!ping</code> proves the bot is
                awake, <code>!itsbagelbot</code> and <code>!source</code> say what it is,
                <code>!bagels</code> counts how many bagels chat has fed it, and
                <code>!bagelboard</code> ranks the feeders. Mods get <code>!cmd</code> (also
                <code>!commands</code>) for the command list, and <code>!nuke</code> when a raid
                needs one line removed from a lot of people at once.
            </p>`,
        },
        {
          kind: 'callout',
          tone: 'note',
          html: `
                <b>Note</b>
                A lead mod is a moderator you promoted in the dashboard, one step above your other
                mods. That is the tier the stream controls sit behind.`,
        },
      ],
    },
  ],
};

export default guide;
