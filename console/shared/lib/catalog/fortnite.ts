// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';
import { FN_SESSION_SAMPLES, FN_SESSION_TOKENS, FN_STATS_SAMPLES, FN_STATS_TOKENS } from './rehearsal-tokens';

export const FORTNITE_MODULE: ModuleDef = 
{
  // !fnstats and !season share one template surface (same tokens, same
  // sample shape) — FN_STATS_TOKENS/FN_STATS_SAMPLES above are the one
  // source of truth, mirroring the Bedwars session commands.
  id: 'fortnite',
  label: 'Fortnite Stats',
  tagline: 'Fortnite BR stats and the daily item shop in chat.',
  description:
    'One command, four looks: !fn shows a player\'s all-time wins, matches, kills, K/D and win rate with a solo/duo/squad breakdown; !fn season shows the same for the current season (the bot tracks season rollovers automatically); !fn session shows wins, kills and K/D since the stream started, snapshotting your standing the moment you go live; !fn store lists what is in today\'s item shop. The squashed forms !fnstats, !fnseason, !fnsession and !fnstore work too. Link your Epic display name below. Viewers can also name any player, e.g. "!fn Ninja"; !fn session always tracks your linked account. PlayStation and Xbox name lookups are not supported yet.',
  icon: 'gamepad',
  category: 'Games',
  defaultEnabled: false,
  replies: [
    {
      key: 'stats',
      label: '!fn',
      tagline: 'All-time Battle Royale stats — !fn, !fn stats or !fnstats.',
      event: '!fn',
      command: 'fn',
      enableKey: 'statsEnabled',
      messageKey: 'statsMessage',
      defaultMessage:
        '{player} all time: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W',
      tokens: FN_STATS_TOKENS,
      previewSamples: FN_STATS_SAMPLES
    },
    {
      key: 'season',
      label: '!fn season',
      tagline: 'Current-season stats (also !fnseason); season rollovers are tracked automatically.',
      event: '!fn season',
      command: 'fn season',
      enableKey: 'seasonEnabled',
      messageKey: 'seasonMessage',
      defaultMessage:
        '{player} this season: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D · solo {solowins}W / duo {duowins}W / squad {squadwins}W',
      tokens: FN_STATS_TOKENS,
      previewSamples: {
        ...FN_STATS_SAMPLES,
        window: 'season',
        wins: '10',
        matches: '21',
        kills: '163',
        kd: '14.8',
        winrate: '47.6',
        solowins: '4',
        solomatches: '7',
        solokd: '12.0',
        duowins: '3',
        duomatches: '9',
        duokd: '9.2',
        squadwins: '3',
        squadmatches: '5',
        squadkd: '21.5'
      }
    },
    {
      key: 'session',
      label: '!fn session',
      tagline: 'Wins, kills and K/D since the stream started (also !fnsession).',
      event: '!fn session',
      command: 'fn session',
      enableKey: 'sessionEnabled',
      messageKey: 'sessionMessage',
      defaultMessage:
        '{player} this stream: {wins} wins in {matches} matches · {winrate}% WR · {kills} kills · {kd} K/D',
      tokens: FN_SESSION_TOKENS,
      previewSamples: FN_SESSION_SAMPLES
    },
    {
      key: 'store',
      label: '!fn store',
      tagline: "Today's item-shop rotation (also !fnstore).",
      event: '!fn store',
      command: 'fn store',
      enableKey: 'storeEnabled',
      messageKey: 'storeMessage',
      defaultMessage: 'Item Shop {date}: {items}',
      tokens: ['date', 'count', 'items'],
      previewSamples: {
        date: '2026-07-09',
        count: '38',
        items: 'Peely Bundle (2800), Renegade Raider (1200), Floss (500) +35 more'
      }
    }
  ],
  settings: [
    {
      key: 'account',
      label: 'Linked account name',
      type: 'text',
      placeholder: 'Your Epic display name',
      help: 'Default player for the stats commands. Leave blank to use your Twitch username.'
    },
    {
      key: 'accountType',
      label: 'Account platform',
      type: 'select',
      placeholder: 'epic',
      options: [
        { value: 'epic', label: 'Epic Games' },
        { value: 'psn', label: 'PlayStation (coming later)' },
        { value: 'xbl', label: 'Xbox Live (coming later)' }
      ],
      help: 'Only Epic display names resolve right now; PlayStation and Xbox lookups come later. Console players: your Epic display name works.'
    }
  ]
};
