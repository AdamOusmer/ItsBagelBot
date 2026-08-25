// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const CLASHROYALE_MODULE: ModuleDef = 
{
  // All four !cr views ride gossip's one shared Clash Royale profile cache,
  // so their token palettes below mirror app/sesame/modules/clashroyale.go.
  id: 'clashroyale',
  label: 'Clash Royale Stats',
  tagline: 'Clash Royale profiles, decks and Path of Legends standing in chat.',
  description:
    'One command, four looks: !cr shows a player\'s lifetime profile (level, win/loss record, win rate, three-crown wins, clan); !cr decks lists their current battle deck with the average elixir cost; !cr ranked shows their Path of Legends standing (falling back to legacy league seasons for older accounts); !cr road shows their trophy-road record and arena. The squashed forms !crstats, !crdecks, !crranked and !crroad work too. Link your player tag below — Clash Royale has no name lookup, so a tag like #P2LQ0GR is required. Viewers can also name any tag, e.g. "!cr #P2LQ0GR".',
  icon: 'crown',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'stats',
      label: '!cr',
      tagline: 'Lifetime profile — !cr, !cr stats or !crstats.',
      event: '!cr',
      command: 'cr',
      enableKey: 'statsEnabled',
      messageKey: 'statsMessage',
      defaultMessage: '{player} · level {level} · {wins}W/{losses}L · {winrate}% WR · {crowns} three-crowns · {clan}',
      tokens: ['player', 'tag', 'level', 'wins', 'losses', 'draws', 'battles', 'winrate', 'crowns', 'challengemax', 'donations', 'totaldonations', 'clan', 'favcard'],
      previewSamples: {
        player: 'Bagel', tag: '#P2LQ0GR', level: '62', wins: '600', losses: '300', draws: '100',
        battles: '1000', winrate: '60', crowns: '120', challengemax: '12', donations: '50',
        totaldonations: '10000', clan: 'Bakery', favcard: 'Knight'
      }
    },
    {
      key: 'decks',
      label: '!cr decks',
      tagline: "The current battle deck with its average elixir (also !crdecks).",
      event: '!cr decks',
      command: 'cr decks',
      enableKey: 'decksEnabled',
      messageKey: 'decksMessage',
      defaultMessage: "{player}'s deck ({count}/8): {cards} · avg elixir {elixir}",
      tokens: ['player', 'tag', 'cards', 'support', 'elixir', 'count'],
      previewSamples: {
        player: 'Bagel', tag: '#P2LQ0GR',
        cards: 'Knight, Archers, Goblins, Giant, P.E.K.K.A, Minions, Fireball, Cannon',
        support: 'Tower Troop', elixir: '3.75', count: '8'
      }
    },
    {
      key: 'ranked',
      label: '!cr ranked',
      tagline: 'Path of Legends standing (also !crranked); legacy league seasons fill in for older records.',
      event: '!cr ranked',
      command: 'cr ranked',
      enableKey: 'rankedEnabled',
      messageKey: 'rankedMessage',
      defaultMessage: '{player} Path of Legends: league {league} · {trophies} trophies · rank #{rank} · best {besttrophies}',
      tokens: ['player', 'tag', 'league', 'trophies', 'rank', 'prevleague', 'prevtrophies', 'bestleague', 'besttrophies', 'bestrank'],
      previewSamples: {
        player: 'Bagel', tag: '#P2LQ0GR', league: '10', trophies: '2100', rank: '321',
        prevleague: '10', prevtrophies: '2050', bestleague: '10', besttrophies: '2400', bestrank: '42'
      }
    },
    {
      key: 'road',
      label: '!cr road',
      tagline: 'Trophy-road record and arena (also !crroad).',
      event: '!cr road',
      command: 'cr road',
      enableKey: 'roadEnabled',
      messageKey: 'roadMessage',
      defaultMessage: '{player}: {trophies} trophies · best {besttrophies} · {arena}',
      tokens: ['player', 'tag', 'trophies', 'besttrophies', 'arena'],
      previewSamples: { player: 'Bagel', tag: '#P2LQ0GR', trophies: '9123', besttrophies: '9345', arena: 'Legendary Arena' }
    }
  ],
  settings: [
    {
      key: 'account',
      label: 'Linked player tag',
      type: 'text',
      placeholder: '#P2LQ0GR',
      help: 'Default player for every command. Clash Royale has no name lookup, so this must be a player tag; leave blank only if you always type one.'
    }
  ]
};
