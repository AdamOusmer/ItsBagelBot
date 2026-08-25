// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shared token palette + preview samples for the Bedwars session commands
// (!daily / !weekly / !monthly) — same template surface, one source of truth.
export const BW_SESSION_TOKENS = [
  'player',
  'wins',
  'losses',
  'finals',
  'finaldeaths',
  'beds',
  'games',
  'levels',
  'fkdr'
];
export const BW_SESSION_SAMPLES: Record<string, string> = {
  player: 'Technoblade',
  wins: '5',
  losses: '2',
  finals: '21',
  finaldeaths: '3',
  beds: '9',
  games: '8',
  levels: '1',
  fkdr: '7.00'
};

// Shared token palette + preview samples for the Fortnite stats commands
// (!fnstats / !season) — same template surface, one source of truth.
export const FN_STATS_TOKENS = [
  'player',
  'window',
  'wins',
  'matches',
  'kills',
  'kd',
  'winrate',
  'solowins',
  'solomatches',
  'solokd',
  'duowins',
  'duomatches',
  'duokd',
  'squadwins',
  'squadmatches',
  'squadkd'
];
export const FN_STATS_SAMPLES: Record<string, string> = {
  player: 'Ninja',
  window: 'lifetime',
  wins: '301',
  matches: '6232',
  kills: '21679',
  kd: '3.66',
  winrate: '4.83',
  solowins: '120',
  solomatches: '2400',
  solokd: '3.2',
  duowins: '90',
  duomatches: '1900',
  duokd: '3.8',
  squadwins: '91',
  squadmatches: '1932',
  squadkd: '4.1'
};

// Token palette + preview samples for !fn session: deltas since the
// stream-start snapshot (no per-mode breakdown, no window — always this
// stream, always the linked account).
export const FN_SESSION_TOKENS = ['player', 'wins', 'matches', 'kills', 'kd', 'winrate'];
export const FN_SESSION_SAMPLES: Record<string, string> = {
  player: 'Ninja',
  wins: '3',
  matches: '12',
  kills: '48',
  kd: '5.33',
  winrate: '25.0'
};
