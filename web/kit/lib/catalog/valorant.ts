// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const VALORANT_MODULE_DEF: ModuleDef = {
  id: 'valorant',
  label: 'Valorant Stats',
  tagline: 'Valorant stats in chat from your signed-in Riot account.',
  description:
    'Sign in with Riot to show your Valorant stats in chat. !val shows your competitive rank, !val matches shows your recent ranked games, and !val lb shows the top 10 of the act leaderboard. Player stats come only from the Riot account you linked.',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'rank',
      label: '!val',
      tagline: 'Competitive rank for your linked account (also !val rank and !valrank).',
      event: '!val',
      command: 'val',
      enableKey: 'rankEnabled',
      messageKey: 'rankMessage',
      defaultMessage: '{player} · {tier} · {rr} RR ({lastchange}) · peak {peaktier}',
      tokens: replyTokens(['player', 'region', 'tier', 'elo', 'rr', 'lastchange', 'peaktier', 'placement'], {
        player: 'Frosty#EUW1', region: 'eu', tier: 'Immortal 2', elo: '1832', rr: '67',
        lastchange: '-12', peaktier: 'Immortal 1', placement: '513'
      })
    },
    {
      key: 'record',
      label: '!val record',
      tagline: 'Ranked games and wins for your linked account (also !valrecord).',
      event: '!val record',
      command: 'val record',
      enableKey: 'recordEnabled',
      messageKey: 'recordMessage',
      defaultMessage: '{player} · {games} ranked games · {wins} wins ({winrate}%)',
      tokens: replyTokens(['player', 'region', 'games', 'wins', 'winrate'], {
        player: 'Frosty#EUW1', region: 'eu', games: '412', wins: '231', winrate: '56'
      })
    },
    {
      key: 'matches',
      label: '!val matches',
      tagline: 'Recent ranked games for your linked account (also !valmatches).',
      event: '!val matches',
      command: 'val matches',
      enableKey: 'matchesEnabled',
      messageKey: 'matchesMessage',
      defaultMessage: "{player}'s last {count}: {matches}",
      tokens: replyTokens(['player', 'region', 'count', 'matches', 'lastago'], {
        player: 'Frosty#EUW1', region: 'eu', count: '2',
        matches: 'Jett 20/14/7 win on Haven, Omen 9/17/3 loss on Ascent', lastago: '2h ago'
      })
    },
    {
      key: 'account',
      label: '!val account',
      tagline: 'Your linked Riot account and its level (also !valaccount).',
      event: '!val account',
      command: 'val account',
      enableKey: 'accountEnabled',
      messageKey: 'accountMessage',
      defaultMessage: '{player} · account level {level}',
      tokens: replyTokens(['player', 'puuid', 'region', 'level', 'card', 'title'], {
        player: 'Frosty#EUW1', puuid: 'a1b2c3d4-05e6-47f8-89a0-b1c2d3e4f5a6',
        region: 'eu', level: '142', card: 'Silver Card', title: 'Radiant'
      })
    },
    {
      key: 'board',
      label: '!val lb',
      tagline: 'Act leaderboard for your linked account (also !vallb).',
      event: '!val lb',
      command: 'val lb',
      enableKey: 'boardEnabled',
      messageKey: 'boardMessage',
      defaultMessage: '{board}: {entries}',
      tokens: replyTokens(['player', 'board', 'count', 'entries'], {
        player: 'Frosty#EUW1', board: 'eu/pc', count: '2',
        entries: '#1 Secret Agent (431 RR), #2 Frosty#EUW1 (402 RR)'
      })
    }
  ],
  settings: [
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
