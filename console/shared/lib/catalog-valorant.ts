// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Valorant module's catalog definition, split out of types.ts for the
// same reason as catalog-games.ts: five customizable replies plus settings
// make it one of the longest MODULE_CATALOG entries, and its token palettes
// mirror app/twitch/sesame/modules/valorant.go (same config keys, same defaults).
import type { ModuleDef } from './types';

export const VALORANT_MODULE_DEF: ModuleDef = {
  // All five !val views are gossip lookups, so their token palettes below
  // mirror app/twitch/sesame/modules/valorant.go.
  id: 'valorant',
  label: 'Valorant Stats',
  tagline: 'Valorant ranks, match history, leaderboards and the daily shop rotation in chat.',
  description:
    'One command, five looks: !val shows a player\'s competitive standing (tier, RR, last game\'s RR change, peak tier); !val matches lists their recent ranked games as agent K/D/A results; !val account shows who a Riot ID resolves to, with their account level; !val lb prints the top 10 of any regional leaderboard, PC or console; !val shop shows today\'s global skin rotation with VP prices and the reset countdown. The squashed forms !valrank, !valmatches, !valaccount, !vallb and !valshop work too. Link your Riot ID below ("Name#Tag") — viewers can also name any player, e.g. "!val Frosty#EUW1", and add a shard or ladder anywhere in the line to scope that one lookup ("!val eu Frosty#EUW1", "!val lb console ap"). Leave the region blank and the bot detects the shard from the account itself.',
  icon: 'crosshair',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'rank',
      label: '!val',
      tagline: 'Competitive standing — !val, !val rank or !valrank.',
      event: '!val',
      command: 'val',
      enableKey: 'rankEnabled',
      messageKey: 'rankMessage',
      defaultMessage: '{player} · {tier} · {rr} RR ({lastchange}) · peak {peaktier}',
      tokens: ['player', 'region', 'tier', 'elo', 'rr', 'lastchange', 'peaktier', 'placement'],
      previewSamples: {
        player: 'Frosty#EUW1', region: 'eu', tier: 'Immortal 2', elo: '1832', rr: '67',
        lastchange: '-12', peaktier: 'Immortal 1', placement: '513'
      }
    },
    {
      key: 'matches',
      label: '!val matches',
      tagline: 'Recent ranked games (also !valmatches).',
      event: '!val matches',
      command: 'val matches',
      enableKey: 'matchesEnabled',
      messageKey: 'matchesMessage',
      defaultMessage: "{player}'s last {count}: {matches}",
      tokens: ['player', 'region', 'count', 'matches', 'lastago'],
      previewSamples: {
        player: 'Frosty#EUW1', region: 'eu', count: '2',
        matches: 'Jett 20/14/7 win on Haven, Omen 9/17/3 loss on Ascent', lastago: '2h ago'
      }
    },
    {
      key: 'account',
      label: '!val account',
      tagline: 'Who an ID resolves to, with account level (also !valaccount).',
      event: '!val account',
      command: 'val account',
      enableKey: 'accountEnabled',
      messageKey: 'accountMessage',
      defaultMessage: '{player} · account level {level}',
      tokens: ['player', 'puuid', 'region', 'level', 'card', 'title'],
      previewSamples: {
        player: 'Frosty#EUW1', puuid: 'a1b2c3d4-05e6-47f8-89a0-b1c2d3e4f5a6',
        region: 'eu', level: '142', card: 'Silver Card', title: 'Radiant'
      }
    },
    {
      key: 'board',
      label: '!val lb',
      tagline: 'Regional top 10, PC or console (also !vallb).',
      event: '!val lb',
      command: 'val lb',
      enableKey: 'boardEnabled',
      messageKey: 'boardMessage',
      defaultMessage: '{board}: {entries}',
      tokens: ['player', 'board', 'count', 'entries'],
      previewSamples: {
        player: 'Frosty#EUW1', board: 'ap/console', count: '2',
        entries: '#4 Zekken#5221 (431 RR), #5 Frosty#EUW1 (402 RR)'
      }
    },
    {
      key: 'shop',
      label: '!val shop',
      tagline: "Today's global skin rotation with reset countdown (also !valshop).",
      event: '!val shop',
      command: 'val shop',
      enableKey: 'shopEnabled',
      messageKey: 'shopMessage',
      defaultMessage: 'Daily rotation ({count}): {items} · resets in {reset}',
      tokens: ['count', 'items', 'reset'],
      previewSamples: {
        count: '2', items: 'Reaver Vandal (1775 VP), Ion Frenzy (875 VP)', reset: '2h 30m'
      }
    }
  ],
  settings: [
    {
      key: 'account',
      label: 'Linked Riot ID',
      type: 'text',
      placeholder: 'Frosty#EUW1',
      help: 'Default player for every command, as "Name#Tag" — Valorant has no name-only lookup, so the #tag is required.'
    },
    {
      key: 'region',
      label: 'Region shard',
      type: 'text',
      placeholder: 'eu',
      help: 'na, eu, ap, kr, br or latam. Leave blank and the bot detects the shard from the account itself.'
    },
    {
      key: 'platform',
      label: 'Ladder',
      type: 'select',
      placeholder: 'pc',
      options: [
        { value: 'pc', label: 'PC' },
        { value: 'console', label: 'Console' }
      ],
      help: 'Ranks and leaderboards are tracked as separate ladders per platform; blank means PC.'
    }
  ]
};
