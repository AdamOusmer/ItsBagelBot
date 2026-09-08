// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "modules" guide in en. Copy only: the structure it fills lives in
// src/lib/guides/skeletons/modules.ts, and every key below is one k('...')
// there. Adding a language is this file translated, with no structure to get
// wrong; a key this locale omits falls back to English.
import type { GuideStrings } from '../../lib/guides/skeleton';

const strings: GuideStrings = {
    'builtins.b0.html': `
            <p>
                Above your own commands, the
                <a href="https://dashboard.itsbagelbot.com/commands" target="_blank" rel="noopener noreferrer">Commands page</a>
                lists nine built-in commands. They arrive with the bot, they can be switched off, and
                they cannot be renamed or deleted.
            </p>`,
    'builtins.b1.caption': 'The nine built-ins, and who chat lets run them.',
    'builtins.b1.head.0': 'Command',
    'builtins.b1.head.1': 'Who can run it',
    'builtins.b1.head.2': 'What it answers',
    'builtins.b1.rows.0.0': '<code>!accountage</code>',
    'builtins.b1.rows.0.1': 'Everyone',
    'builtins.b1.rows.0.2': 'How old a Twitch account is.',
    'builtins.b1.rows.1.0': '<code>!followage</code>',
    'builtins.b1.rows.1.1': 'Everyone',
    'builtins.b1.rows.1.2': 'How long someone has followed you.',
    'builtins.b1.rows.2.0': '<code>!uptime</code>',
    'builtins.b1.rows.2.1': 'Everyone',
    'builtins.b1.rows.2.2': 'How long the current stream has been live.',
    'builtins.b1.rows.3.0': '<code>!clip</code>',
    'builtins.b1.rows.3.1': 'Everyone, live only',
    'builtins.b1.rows.3.2': 'Clips the last moments and posts the link. <code>!clip30</code> makes a 30 second clip.',
    'builtins.b1.rows.4.0': '<code>!title</code> <code>!settitle</code>',
    'builtins.b1.rows.4.1': 'Lead mod',
    'builtins.b1.rows.4.2': 'Reads the stream title, or sets a new one.',
    'builtins.b1.rows.5.0': '<code>!game</code> <code>!setgame</code>',
    'builtins.b1.rows.5.1': 'Lead mod',
    'builtins.b1.rows.5.2': 'Reads the category, or sets a new one.',
    'builtins.b1.rows.6.0': '<code>!tags</code> <code>!settags</code>',
    'builtins.b1.rows.6.1': 'Lead mod',
    'builtins.b1.rows.6.2': 'Reads the stream tags, or replaces them.',
    'builtins.b1.rows.7.0': '<code>!commercial</code> <code>!ad</code>',
    'builtins.b1.rows.7.1': 'Lead mod, live only',
    'builtins.b1.rows.7.2': 'Starts an ad break.',
    'builtins.b1.rows.8.0': '<code>!marker</code>',
    'builtins.b1.rows.8.1': 'Lead mod, live only',
    'builtins.b1.rows.8.2': 'Drops a marker you can find later in the VOD.',
    'builtins.b2.html': `
            <p>
                A few more answer without appearing anywhere: <code>!ping</code> proves the bot is
                awake, <code>!itsbagelbot</code> and <code>!source</code> say what it is,
                <code>!bagels</code> counts how many bagels chat has fed it, and
                <code>!bagelboard</code> ranks the feeders. Mods get <code>!cmd</code> (also
                <code>!commands</code>) for the command list, and <code>!nuke</code> when a raid
                needs one line removed from a lot of people at once.
            </p>`,
    'builtins.b3.html': `
                <b>Note</b>
                A lead mod is a moderator you promoted in the dashboard, one step above your other
                mods. That is the tier the stream controls sit behind.`,
    'builtins.heading': 'Always on',
    'builtins.note': 'Nine commands ship with the bot, plus a handful of small ones.',
    'catalog.b0.html': `
            <p>
                Here is the full list, the same one the dashboard draws. Each card carries the
                module's own one-line description, its category, whether it starts on or off, what
                you have to set up first, and the chat commands it adds. Pick a category chip, or
                type into the filter: it matches names, descriptions and commands, so
                <code>!sr</code> finds Song Requests and "elo" finds MCSR Ranked.
            </p>`,
    'catalog.b1.labels.all': 'All',
    'catalog.b1.labels.commands': 'Commands',
    'catalog.b1.labels.countAll': '{n} modules',
    'catalog.b1.labels.countCat': '{n} in {cat}',
    'catalog.b1.labels.countMatch': '{shown} of {total}',
    'catalog.b1.labels.empty': 'Nothing matches that. Try a command like !sr, or a game name.',
    'catalog.b1.labels.free': 'Free',
    'catalog.b1.labels.needs': 'Needs',
    'catalog.b1.labels.premium': 'Premium beta',
    'catalog.b1.labels.searchLabel': 'Filter modules',
    'catalog.b1.labels.searchPlaceholder': 'Name, command, or what it does',
    'catalog.b1.props.categories.0': 'Moderation',
    'catalog.b1.props.categories.1': 'Chat',
    'catalog.b1.props.categories.2': 'Channel',
    'catalog.b1.props.categories.3': 'Points',
    'catalog.b1.props.categories.4': 'Play',
    'catalog.b1.props.categories.5': 'Gear',
    'catalog.b1.props.categories.6': 'Stats',
    'catalog.b1.props.modules.0.cat': 'Moderation',
    'catalog.b1.props.modules.0.name': 'AutoMod',
    'catalog.b1.props.modules.0.plan': 'premium',
    'catalog.b1.props.modules.0.start': 'On by default',
    'catalog.b1.props.modules.0.tagline': 'Catch scams, IP-grabbers and raid spam before your mods do.',
    'catalog.b1.props.modules.1.cat': 'Chat',
    'catalog.b1.props.modules.1.name': 'Trigger Words',
    'catalog.b1.props.modules.1.start': 'Off by default',
    'catalog.b1.props.modules.1.tagline': 'Auto-reply when a word shows up in chat, no "!" needed.',
    'catalog.b1.props.modules.10.cat': 'Points',
    'catalog.b1.props.modules.10.commands': '!points, !points give @user 50, !leaderboard, !points set @user 500, !points add @user 100, !points remove @user 100',
    'catalog.b1.props.modules.10.name': 'Loyalty Points',
    'catalog.b1.props.modules.10.start': 'Off by default',
    'catalog.b1.props.modules.10.tagline': 'Viewers earn channel currency for subs, cheers and watch time.',
    'catalog.b1.props.modules.11.cat': 'Points',
    'catalog.b1.props.modules.11.commands': '!counter',
    'catalog.b1.props.modules.11.name': 'Counters',
    'catalog.b1.props.modules.11.start': 'Always on',
    'catalog.b1.props.modules.11.tagline': 'Track wins, deaths, hugs, redeems, anything your chat can count.',
    'catalog.b1.props.modules.12.cat': 'Points',
    'catalog.b1.props.modules.12.commands': '!gamble <amount>, !gamble half, !gamble all',
    'catalog.b1.props.modules.12.name': 'Gamble',
    'catalog.b1.props.modules.12.needs': 'Loyalty Points on. Gamble is a row on the Loyalty page.',
    'catalog.b1.props.modules.12.start': 'Off by default',
    'catalog.b1.props.modules.12.tagline': 'Let viewers wager their points on a roll with !gamble.',
    'catalog.b1.props.modules.13.cat': 'Points',
    'catalog.b1.props.modules.13.commands': '!duel, !duel <stake>, !duel <user> <stake>, !duel accept, !duel decline, !duel cancel',
    'catalog.b1.props.modules.13.name': 'Duels',
    'catalog.b1.props.modules.13.needs': 'Loyalty Points on. Duels is a row on the Loyalty page.',
    'catalog.b1.props.modules.13.start': 'Off by default',
    'catalog.b1.props.modules.13.tagline': 'Viewer-vs-viewer point duels: pot free-for-alls and 1v1 challenges.',
    'catalog.b1.props.modules.14.cat': 'Play',
    'catalog.b1.props.modules.14.commands': '!join, !leave, !list, !queuelist, !queue, !queue open, !queue close, !queue next, !queue remove <user>, !queue clear',
    'catalog.b1.props.modules.14.name': 'Play Queue',
    'catalog.b1.props.modules.14.start': 'Off by default',
    'catalog.b1.props.modules.14.tagline': 'Let viewers line up to play with you, first come first served.',
    'catalog.b1.props.modules.15.cat': 'Play',
    'catalog.b1.props.modules.15.commands': '!join, !claim, !winner, !raffle, !raffle open [minutes] [winners] [remind], !raffle draw [winners], !raffle close, !raffle cancel',
    'catalog.b1.props.modules.15.name': 'Raffle',
    'catalog.b1.props.modules.15.needs': 'Raffle owns !join when Play Queue is on as well.',
    'catalog.b1.props.modules.15.start': 'Off by default',
    'catalog.b1.props.modules.15.tagline': 'Timed random draws your chat enters with !join.',
    'catalog.b1.props.modules.16.cat': 'Gear',
    'catalog.b1.props.modules.16.commands': '!sr <song or link>, !remove, !srlist, !songlist, !current, !song, !nowplaying, !np, !skip, !next, !clear',
    'catalog.b1.props.modules.16.name': 'Song Requests',
    'catalog.b1.props.modules.16.needs': 'Your own Spotify app, plus a Spotify login.',
    'catalog.b1.props.modules.16.start': 'Off by default',
    'catalog.b1.props.modules.16.tagline': '!sr and a channel-points reward that queue songs from Spotify.',
    'catalog.b1.props.modules.17.cat': 'Gear',
    'catalog.b1.props.modules.17.name': 'Govee Lights',
    'catalog.b1.props.modules.17.needs': 'A Govee API key and a channel-point reward. Works while you are live.',
    'catalog.b1.props.modules.17.start': 'Off by default',
    'catalog.b1.props.modules.17.tagline': 'Let viewers recolour your Govee lights with channel points.',
    'catalog.b1.props.modules.18.cat': 'Gear',
    'catalog.b1.props.modules.18.name': 'Discord',
    'catalog.b1.props.modules.18.needs': 'A Discord server connected, or one created from the template.',
    'catalog.b1.props.modules.18.plan': 'premium',
    'catalog.b1.props.modules.18.start': 'Off by default',
    'catalog.b1.props.modules.18.tagline': 'One bot on Twitch and Discord. Go-live, clips, welcomes, tickets, and voice.',
    'catalog.b1.props.modules.19.cat': 'Stats',
    'catalog.b1.props.modules.19.commands': '!daily, !bwdaily, !weekly, !bwweekly, !monthly, !bwmonthly, !bwstats, !bedwars, !sniper, !urchin, !tag, !tags, !bwtags, !tagdescription',
    'catalog.b1.props.modules.19.name': 'Bedwars Stats',
    'catalog.b1.props.modules.19.needs': 'Your Minecraft username.',
    'catalog.b1.props.modules.19.start': 'Off by default',
    'catalog.b1.props.modules.19.tagline': 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
    'catalog.b1.props.modules.2.cat': 'Chat',
    'catalog.b1.props.modules.2.commands': '!time',
    'catalog.b1.props.modules.2.name': 'Local Time',
    'catalog.b1.props.modules.2.needs': 'A timezone, picked on the module page.',
    'catalog.b1.props.modules.2.start': 'Off by default',
    'catalog.b1.props.modules.2.tagline': 'Viewers ask what time it is for you with !time.',
    'catalog.b1.props.modules.20.cat': 'Stats',
    'catalog.b1.props.modules.20.commands': '!elo [player], !lastmatch, !record <a> <b>, !matchrecord, !lb, !leaderboard, !rankedlb, !session, !mcsrsession, !race, !weeklyrace, !pb, !personalbest daily, !pace, !nethers, !lastfort',
    'catalog.b1.props.modules.20.name': 'MCSR Ranked',
    'catalog.b1.props.modules.20.needs': 'Your Minecraft username, an MCSR Ranked account and a PaceMan account.',
    'catalog.b1.props.modules.20.start': 'Off by default',
    'catalog.b1.props.modules.20.tagline': 'Ranked elo and per-stream session stats for MCSR runners.',
    'catalog.b1.props.modules.21.cat': 'Stats',
    'catalog.b1.props.modules.21.commands': '!fn [player], !fnstats, !fortnitestats, !fnseason, !fnsession, !fnstore, !itemshop, !fnshop',
    'catalog.b1.props.modules.21.name': 'Fortnite Stats',
    'catalog.b1.props.modules.21.needs': 'Your Epic display name.',
    'catalog.b1.props.modules.21.start': 'Off by default',
    'catalog.b1.props.modules.21.tagline': 'Fortnite BR stats and the daily item shop in chat.',
    'catalog.b1.props.modules.22.cat': 'Stats',
    'catalog.b1.props.modules.22.commands': '!cr [tag], !crstats, !clashroyale, !crdecks, !crdeck, !crranked, !crpol, !crroad, !crtrophy',
    'catalog.b1.props.modules.22.name': 'Clash Royale Stats',
    'catalog.b1.props.modules.22.needs': 'Your Supercell player tag, like #P2LQ0GR.',
    'catalog.b1.props.modules.22.start': 'Off by default',
    'catalog.b1.props.modules.22.tagline': 'Clash Royale profiles, decks and Path of Legends standing in chat.',
    'catalog.b1.props.modules.23.cat': 'Stats',
    'catalog.b1.props.modules.23.commands': '!val [RiotID], !valrank, !valmatches, !valhistory, !valaccount, !valwho, !vallb, !valleaderboard, !valshop, !valrotation',
    'catalog.b1.props.modules.23.name': 'Valorant Stats',
    'catalog.b1.props.modules.23.needs': 'Your Riot ID, like Frosty#EUW1, and your region.',
    'catalog.b1.props.modules.23.start': 'Off by default',
    'catalog.b1.props.modules.23.tagline': 'Valorant ranks, match history, leaderboards and the daily shop rotation in chat.',
    'catalog.b1.props.modules.3.cat': 'Chat',
    'catalog.b1.props.modules.3.commands': '!quote, !quotes, !quote <n>, !quote <word>, !addquote, !quoteadd, !quote edit <n> <text>, !quote remove <n>',
    'catalog.b1.props.modules.3.name': 'Quotes',
    'catalog.b1.props.modules.3.start': 'Off by default',
    'catalog.b1.props.modules.3.tagline': 'Save the best things said on stream and replay them in chat.',
    'catalog.b1.props.modules.4.cat': 'Chat',
    'catalog.b1.props.modules.4.name': 'Timers',
    'catalog.b1.props.modules.4.needs': 'A live stream. Timers stay quiet when you are offline.',
    'catalog.b1.props.modules.4.start': 'Off by default',
    'catalog.b1.props.modules.4.tagline': 'Post repeating chat messages on a schedule while you are live.',
    'catalog.b1.props.modules.5.cat': 'Chat',
    'catalog.b1.props.modules.5.name': 'Emote Pyramids & Streaks',
    'catalog.b1.props.modules.5.start': 'Off by default',
    'catalog.b1.props.modules.5.tagline': 'Celebrate chat-built emote pyramids and emote streaks.',
    'catalog.b1.props.modules.6.cat': 'Channel',
    'catalog.b1.props.modules.6.name': 'Chat Alerts',
    'catalog.b1.props.modules.6.needs': 'The Twitch permissions you granted at login.',
    'catalog.b1.props.modules.6.start': 'On by default',
    'catalog.b1.props.modules.6.tagline': 'Announce follows, subs, cheers, raids and ad breaks in chat.',
    'catalog.b1.props.modules.7.cat': 'Channel',
    'catalog.b1.props.modules.7.name': 'Auto Shoutout',
    'catalog.b1.props.modules.7.needs': 'Mod permission, if you also want the native Twitch /shoutout.',
    'catalog.b1.props.modules.7.start': 'Off by default',
    'catalog.b1.props.modules.7.tagline': 'Welcome incoming raids with an automatic shoutout.',
    'catalog.b1.props.modules.8.cat': 'Channel',
    'catalog.b1.props.modules.8.name': 'Channel Points',
    'catalog.b1.props.modules.8.needs': 'Affiliate or partner. The bot creates the rewards itself.',
    'catalog.b1.props.modules.8.start': 'Off by default',
    'catalog.b1.props.modules.8.tagline': 'Turn channel-point redemptions into bot actions.',
    'catalog.b1.props.modules.9.cat': 'Channel',
    'catalog.b1.props.modules.9.commands': '!title, !settitle, !game, !setgame, !tags, !settags, !commercial, !ad, !marker, !cmd, !cmds, !command, !commands',
    'catalog.b1.props.modules.9.name': 'Stream Management',
    'catalog.b1.props.modules.9.start': 'Always on',
    'catalog.b1.props.modules.9.tagline': 'Set the live title, category and tags, run ads, and drop markers from chat.',
    'catalog.b2.html': `
                <b>Tip</b>
                The same search sits at the top of the dashboard page. If a viewer asks for something
                and you cannot remember which module owns it, type the command there.`,
    'catalog.heading': 'Every module',
    'catalog.note': 'The whole catalogue, filtered by category or by a command.',
    'configure.b0.html': `
            <p>
                Configure opens the module's own page. It always has the same three parts: the
                module status at the top, the settings, and the replies it posts in chat. Chat
                Alerts is a good one to learn on, because it has six replies with six switches:
                follow, subscribe, gift sub, cheer, raid, and ad break.
            </p>`,
    'configure.b1.caption': 'Chat Alerts: module status, the six replies, and the editor docked on the right.',
    'configure.b1.notes.0.text': 'Module status. One switch for the whole module, and turning it off keeps every setting.',
    'configure.b1.notes.1.text': 'Each reply is a row you can open, with a switch of its own. Chat Alerts has six.',
    'configure.b1.notes.2.text': 'The ad break alert ships off. Turn it on if you want chat warned before the ads roll.',
    'configure.b1.notes.3.text': 'The message. Braces are tokens the bot fills in: {user} here, {bits} on the cheer alert.',
    'configure.b1.notes.4.text': 'The rehearsal, same as the command editor. You watch the line land before a viewer does.',
    'configure.b2.html': `
            <p>
                Every reply is a template. <code>&#123;user&#125;</code> is the viewer who set it off,
                and each alert adds its own: <code>&#123;tier&#125;</code> on subs,
                <code>&#123;count&#125;</code> on gift subs, <code>&#123;bits&#125;</code> on cheers,
                <code>&#123;viewers&#125;</code> on raids, <code>&#123;duration&#125;</code> on ad
                breaks. The editor lists the tokens that reply accepts, and the catalogue above tells
                you which modules have replies to rewrite. Leave a message blank and the bot uses the
                default line.
            </p>`,
    'configure.b3.html': `
                <b>Note</b>
                A follow alert fires at most once per viewer every three days, so someone unfollowing
                and refollowing cannot spam your chat.`,
    'configure.heading': 'Configure a module',
    'configure.note': 'Chat Alerts, its six replies, and the rehearsal under the editor.',
    'games.b0.html': `
            <p>
                Five modules answer "what rank are you?" so you do not have to. Each one asks for a
                single account field on its page, then every reply is a template with that game's
                own tokens. Aliases are generous: the short spelling and the long one both work.
            </p>`,
    'games.b1.items.0.chips.0': 'Hypixel',
    'games.b1.items.0.html': `
                <p>Set your Minecraft username. Seven reply templates, cooldown per command.</p>
                <p><code>!daily</code> <code>!weekly</code> <code>!monthly</code> <code>!bwstats</code> <code>!sniper</code> <code>!tag</code></p>
                <p>sesame_sam today: 12W 4L · 210 finals · 34 beds · 3.1 FKDR</p>`,
    'games.b1.items.0.title': 'Bedwars Stats',
    'games.b1.items.1.chips.0': 'Minecraft',
    'games.b1.items.1.html': `
                <p>Set your Minecraft username. Needs an MCSR Ranked account and a PaceMan account. Per-command toggles.</p>
                <p><code>!elo</code> <code>!session</code> <code>!lastmatch</code> <code>!record</code> <code>!lb</code> <code>!pace</code> <code>!pb</code></p>
                <p>sesame_sam: 1650 elo · rank #12 · 40W 20L this season</p>`,
    'games.b1.items.1.title': 'MCSR Ranked',
    'games.b1.items.2.chips.0': 'Epic',
    'games.b1.items.2.html': `
                <p>Set your Epic display name. Account type defaults to Epic.</p>
                <p><code>!fn</code> <code>!fnstats</code> <code>!fnseason</code> <code>!fnsession</code> <code>!fnstore</code></p>
                <p>Item Shop 2026-09-07: Renegade Raider, Aerial Assault Trooper, Take the L</p>`,
    'games.b1.items.2.title': 'Fortnite Stats',
    'games.b1.items.3.chips.0': 'Supercell',
    'games.b1.items.3.html': `
                <p>Set your Supercell player tag, the one that looks like #P2LQ0GR.</p>
                <p><code>!cr</code> <code>!crstats</code> <code>!crdecks</code> <code>!crranked</code> <code>!crroad</code></p>
                <p>sesame_sam · level 42 · 5120W/4380L · 54% WR · 1180 three-crowns · Crust Clan</p>`,
    'games.b1.items.3.title': 'Clash Royale Stats',
    'games.b1.items.4.chips.0': 'Riot',
    'games.b1.items.4.html': `
                <p>Set your Riot ID and region. Region defaults to eu, platform to pc.</p>
                <p><code>!val</code> <code>!valrank</code> <code>!valmatches</code> <code>!vallb</code> <code>!valshop</code></p>
                <p>Frosty#EUW1 · Immortal 2 · 143 RR (+21) · peak Immortal 3</p>`,
    'games.b1.items.4.title': 'Valorant Stats',
    'games.b2.html': `
                <b>Watch out</b>
                Two spellings people get wrong. The Fortnite season command is
                <code>!fnseason</code>, written as one word. And the MCSR session resets the moment
                your stream goes live, so <code>!session</code> answers for tonight, not for the week.`,
    'games.heading': 'Game stats in chat',
    'games.note': 'Five modules, one account field each, one command your chat will wear out.',
    'gear.b0.html': `
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
    'gear.b1.html': `
            <h3>Govee Lights</h3>
            <p>
                Viewers spend channel points to recolour the Govee lights in your room. You paste a
                Govee API key, pick the device, and bind a reward. The key is encrypted and never
                shown back to you, not even to the dashboard. To get one: Govee Home app &gt;
                Profile &gt; gear &gt; "Apply for API Key".
            </p>`,
    'gear.b2.html': `
                <b>Watch out</b>
                Lights only answer while you are live. A redemption that lands off stream is refunded
                automatically, so nobody pays for a dark room.`,
    'gear.b3.html': `
            <h3>Discord</h3>
            <p>
                The same bot on both sides: go-live posts, clips, welcomes, tickets and voice rooms.
                Discord skips the modules grid and says nothing in Twitch chat; it gets a page of
                its own in the dashboard. Connect a server you already run, or let the bot
                build one from the template. It is premium while the beta lasts, and what you
                configure during the beta keeps working afterwards.
            </p>`,
    'gear.heading': 'Song requests, lights, Discord',
    'gear.note': 'The three modules that reach outside Twitch, and what each one asks for first.',
    'meta.card.chips.0': 'moderation',
    'meta.card.chips.1': 'chat',
    'meta.card.chips.2': 'points',
    'meta.card.chips.3': 'stats',
    'meta.card.description': 'The 24 tiles on your Modules page: what each one does, what it needs before it works, and which commands it brings to chat.',
    'meta.card.meta': '10 min · 7 sections',
    'meta.card.title': 'Modules',
    'meta.description': 'The ItsBagelBot modules page explained: the seven categories, the tile shapes, how a module is configured, points and games, game stats in chat, and the nine built-in commands.',
    'meta.eyebrow': 'Guide',
    'meta.heading': 'Modules',
    'meta.lead': 'Every feature on your channel is a tile with a switch. Here is what each one does, what it needs first, and what the odd tiles mean.',
    'meta.minutes': '10 min read',
    'meta.title': 'Modules - ItsBagelBot Guides',
    'page.b0.html': `
            <p>
                A module is one feature of the bot with its own switch. The
                <a href="https://dashboard.itsbagelbot.com/modules" target="_blank" rel="noopener noreferrer">Modules page</a>
                lists them as tiles, grouped by the Categories rail on the left: Moderation, Chat,
                Channel, Points, Play, Gear, Stats. The line under the title counts what is running,
                and the search box matches a module name, what it does, or a chat command you half
                remember.
            </p>`,
    'page.b1.caption': 'The Modules page: the Categories rail, tiles with a Configure button, and the switch.',
    'page.b1.notes.0.text': 'The Categories rail. Seven groups, in this order, and clicking one scrolls the grid to it.',
    'page.b1.notes.1.text': 'A tile is one module: its name, its category, and the one line the dashboard uses to describe it.',
    'page.b1.notes.2.text': "Configure opens the module's own page, where its settings and its chat lines live.",
    'page.b1.notes.3.text': 'The switch. Off means the module says nothing, and every setting stays where you left it.',
    'page.b1.notes.4.text': 'AutoMod carries a "Beta · Premium" chip. On a free channel the tile is locked.',
    'page.b1.notes.5.text': 'Counters has no switch. It is always on, and so is Stream Management.',
    'page.b2.html': `
            <p>
                Most tiles behave the same way: flip the switch, click Configure, done. Five tiles
                behave differently, and knowing which is which saves you hunting for a switch that
                was never there.
            </p>`,
    'page.b3.caption': 'Six tile shapes, five of which surprise people at least once.',
    'page.b3.head.0': 'Tile shape',
    'page.b3.head.1': 'What you see',
    'page.b3.head.2': 'Which modules',
    'page.b3.rows.0.0': 'Ordinary',
    'page.b3.rows.0.1': 'A switch and a Configure button. The switch turns the feature on for your channel.',
    'page.b3.rows.0.2': 'Timers, Quotes, Raffle, and most of the grid.',
    'page.b3.rows.1.0': 'Hidden',
    'page.b3.rows.1.1': 'The module runs inside the bot and never reaches the grid, because it has nothing for you to set.',
    'page.b3.rows.1.2': 'The internal plumbing behind commands.',
    'page.b3.rows.2.0': 'Section',
    'page.b3.rows.2.1': 'The module skips the grid and gets a page of its own in the dashboard.',
    'page.b3.rows.2.2': 'Discord.',
    'page.b3.rows.3.0': 'Nested',
    'page.b3.rows.3.1': `A row on the parent module's page, with no switch of its own. The row says: "This game spends <code>&#123;parent&#125;</code>. Turn it on from there. It cannot run on its own."`,
    'page.b3.rows.3.2': 'Gamble and Duels, on the Loyalty Points page.',
    'page.b3.rows.4.0': 'Always on',
    'page.b3.rows.4.1': 'A tile with a Configure button, and the switch is missing on purpose. The feature runs whatever you do.',
    'page.b3.rows.4.2': 'Counters, Stream Management.',
    'page.b3.rows.5.0': 'Beta',
    'page.b3.rows.5.1': 'A locked tile with a "Beta · Premium" chip, and the settings underneath once you have Premium.',
    'page.b3.rows.5.2': 'AutoMod, Discord.',
    'page.b4.html': `
                <b>Note</b>
                Two modules are premium while they are in beta: AutoMod and Discord. Everything else
                on this page works on the free plan, for as long as you like.`,
    'page.heading': 'The modules page',
    'page.note': 'Seven categories, one switch per tile, six tile shapes.',
    'points.b0.html': `
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
    'points.b1.caption': 'The numbers the bot ships with. Every one of them is yours to change.',
    'points.b1.head.0': 'Game',
    'points.b1.head.1': 'Defaults',
    'points.b1.head.2': 'Commands',
    'points.b1.rows.0.0': 'Gamble',
    'points.b1.rows.0.1': 'Win chance 50%, adjustable from 1 to 99. Minimum bet 1, maximum 1000. Cooldown 10 s per viewer.',
    'points.b1.rows.0.2': '<code>!gamble 100</code>, <code>!gamble half</code>, <code>!gamble all</code>',
    'points.b1.rows.1.0': 'Duels',
    'points.b1.rows.1.1': 'Stakes from 1 to 1000. A pot stays open 60 s. A named challenge waits 120 s for an answer.',
    'points.b1.rows.1.2': '<code>!duel</code>, <code>!duel 500</code>, <code>!duel @ferret_king 500</code>, <code>!duel accept</code>',
    'points.b2.caption': 'A roll that pays, a roll that does not, and a duel that ends badly for one of them.',
    'points.b2.lines.0.name': 'sesame_sam',
    'points.b2.lines.0.text': '!gamble 100',
    'points.b2.lines.1.text': '@sesame_sam rolled 37 (needed 50 or less) and won 100 bagels, now at 480!',
    'points.b2.lines.2.name': 'ferret_king',
    'points.b2.lines.2.text': '!gamble 250',
    'points.b2.lines.3.text': '@ferret_king rolled 88 (needed 50 or less) and lost 250 bagels. Now at 90.',
    'points.b2.lines.4.name': 'maya_live',
    'points.b2.lines.4.text': '!duel @ferret_king 500',
    'points.b2.lines.5.text': '@maya_live challenges @ferret_king for 500 bagels! @ferret_king, type !duel accept within 120s. Winner takes 1000!',
    'points.b2.lines.6.name': 'ferret_king',
    'points.b2.lines.6.text': '!duel accept',
    'points.b2.lines.7.text': 'The blades fall: @maya_live defeats @ferret_king and takes 1000 bagels!',
    'points.b2.title': '#your_channel',
    'points.b3.html': `
                <b>Watch out</b>
                Looking for Gamble or Duels on the modules grid is a wasted trip. Turn on Loyalty
                Points, open its page, and switch the games on from the rows there.`,
    'points.heading': 'Points, gamble and duels',
    'points.note': 'Loyalty is the parent. Gamble and Duels are rows on its page.',
};

export default strings;
