// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const MCSR_MODULE: ModuleDef = 
{
  id: 'mcsr',
  label: 'MCSR Ranked',
  tagline: 'Ranked elo and per-stream session stats for MCSR runners.',
  description:
    'Viewer commands backed by the MCSR Ranked API: !elo shows the current rating and season record; !session shows elo and wins/losses since the stream started, snapshotting your standing the moment you go live. !elo can name any player (e.g. "!elo Feinberg"); !session always tracks your linked account. !lastmatch shows the most recent match; !record compares two players\' head-to-head record; !lb shows the top 5 of the elo, phase-point or record leaderboard; !race shows the weekly race leader and your own time. !elo, !lastmatch, !record and !lb all accept a trailing "season:11" to look at a past season. !pace, !nethers and !lastfort pull live speedrun splits from PaceMan.gg for the same linked account. !pb shows a personal best: daily/weekly/monthly from PaceMan, or "ranked" for the MCSR Ranked season best; a player name can follow any of those, or stand alone for the all-time PaceMan best.',
  icon: 'pickaxe',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'elo',
      label: '!elo',
      tagline: 'Current elo, rank and season record.',
      event: '!elo',
      command: 'elo',
      enableKey: 'eloEnabled',
      messageKey: 'eloMessage',
      defaultMessage: '{player}: {elo} elo · rank #{rank} · {wins}W {losses}L this season',
      tokens: ['player', 'elo', 'rank', 'wins', 'losses', 'matches', 'country'],
      previewSamples: {
        player: 'Feinberg',
        elo: '1650',
        rank: '12',
        wins: '40',
        losses: '20',
        matches: '61',
        country: 'us'
      }
    },
    {
      key: 'session',
      label: '!session',
      tagline: 'Elo and record since the stream started.',
      event: '!session',
      command: 'session',
      enableKey: 'sessionEnabled',
      messageKey: 'sessionMessage',
      defaultMessage: '{player} this stream: {elochange} elo ({elo} now) · {wins}W {losses}L in {matches} matches',
      tokens: ['player', 'elo', 'elochange', 'wins', 'losses', 'matches'],
      previewSamples: {
        player: 'Feinberg',
        elo: '1660',
        elochange: '+24',
        wins: '3',
        losses: '1',
        matches: '4'
      }
    },
    {
      key: 'pace',
      label: '!pace',
      tagline: 'Split averages for the current session, from PaceMan.gg.',
      event: '!pace',
      command: 'pace',
      enableKey: 'paceEnabled',
      messageKey: 'paceMessage',
      defaultMessage:
        '{player} this session: {nethers} nethers (avg {nether}) · bastion {bastion} · fortress {fortress} · fp {firstportal} · {nph} nph',
      tokens: [
        'player',
        'nethers',
        'nether',
        'bastion',
        'fortress',
        'firststructure',
        'secondstructure',
        'firstportal',
        'stronghold',
        'end',
        'finish',
        'nph'
      ],
      previewSamples: {
        player: 'Feinberg',
        nethers: '3',
        nether: '1:42',
        bastion: '3:55',
        fortress: '7:12',
        firststructure: '3:55',
        secondstructure: '7:12',
        firstportal: '9:20',
        stronghold: '12:05',
        end: '13:50',
        finish: '0:00',
        nph: '5.3'
      }
    },
    {
      key: 'nethers',
      label: '!nethers',
      tagline: 'Nether-entrance count and pace for the current session.',
      event: '!nethers',
      command: 'nethers',
      enableKey: 'nethersEnabled',
      messageKey: 'nethersMessage',
      defaultMessage: '{player}: {nethers} nethers this session (avg {nether}) · {nph} nph',
      tokens: ['player', 'nethers', 'nether', 'nph'],
      previewSamples: { player: 'Feinberg', nethers: '3', nether: '1:42', nph: '5.3' }
    },
    {
      key: 'lastfort',
      label: '!lastfort',
      tagline: 'Splits for the most recent run that reached a fortress or bastion.',
      event: '!lastfort',
      command: 'lastfort',
      enableKey: 'lastFortEnabled',
      messageKey: 'lastFortMessage',
      defaultMessage:
        '{player} last fort: nether {nether} · bastion {bastion} · fortress {fortress} · fp {firstportal} · sh {stronghold} · {ago} ago',
      tokens: [
        'player',
        'nether',
        'bastion',
        'fortress',
        'firstportal',
        'stronghold',
        'end',
        'finish',
        'ago'
      ],
      previewSamples: {
        player: 'Feinberg',
        nether: '1:42',
        bastion: '3:55',
        fortress: '7:12',
        firstportal: '9:20',
        stronghold: '—',
        end: '—',
        finish: '—',
        ago: '12m'
      }
    },
    {
      key: 'lastmatch',
      label: '!lastmatch',
      tagline: 'The most recent match: opponent, result, seed and elo change.',
      event: '!lastmatch',
      command: 'lastmatch',
      enableKey: 'lastMatchEnabled',
      messageKey: 'lastMatchMessage',
      defaultMessage:
        '{player} vs {opponent}: {result} · {time} · {seed} {structure} · {elochange} elo · {ago} ago',
      tokens: ['player', 'opponent', 'result', 'time', 'seed', 'structure', 'elochange', 'ago'],
      previewSamples: {
        player: 'Feinberg',
        opponent: 'lowk3y_',
        result: 'won',
        time: '11:03.135',
        seed: 'Desert Temple',
        structure: 'Treasure',
        elochange: '+21',
        ago: '2m'
      }
    },
    {
      key: 'record',
      label: '!record',
      tagline: 'Head-to-head wins between two players.',
      event: '!record <player> [player2]',
      command: 'record',
      previewArgs: 'Feinberg lowk3y_',
      enableKey: 'recordEnabled',
      messageKey: 'recordMessage',
      defaultMessage: '{playera} {winsa} - {winsb} {playerb} · {played} played',
      tokens: ['playera', 'playerb', 'winsa', 'winsb', 'played'],
      previewSamples: { playera: 'Feinberg', playerb: 'lowk3y_', winsa: '20', winsb: '14', played: '34' }
    },
    {
      key: 'lb',
      label: '!lb',
      tagline: 'Top 5 of the elo, phase-point or record leaderboard.',
      event: '!lb [phase [predicted]|record] [country:xx]',
      command: 'lb',
      enableKey: 'lbEnabled',
      messageKey: 'lbMessage',
      defaultMessage: '{board}: {list}',
      tokens: ['board', 'list'],
      previewSamples: { board: 'Elo', list: '#1 Feinberg 2464 · #2 lowk3y_ 2436' }
    },
    {
      key: 'race',
      label: '!race',
      tagline: "This week's race leader and the player's own time.",
      event: '!race',
      command: 'race',
      enableKey: 'raceEnabled',
      messageKey: 'raceMessage',
      defaultMessage: '#1 {leader} ({leadertime}) · {player}: {time} (#{rank})',
      tokens: ['leader', 'leadertime', 'player', 'time', 'rank'],
      previewSamples: { leader: 'gharfyy', leadertime: '2:27.374', player: 'Feinberg', time: '2:40.000', rank: '2' }
    },
    {
      key: 'pb',
      label: '!pb',
      tagline: 'Personal best time: PaceMan daily/weekly/monthly/all-time, or the MCSR Ranked season best.',
      event: '!pb [daily|weekly|monthly|ranked] [player]',
      command: 'pb',
      previewArgs: 'daily',
      enableKey: 'pbEnabled',
      messageKey: 'pbMessage',
      defaultMessage: '{player}: {time} ({window} PB)',
      tokens: ['player', 'time', 'window'],
      previewSamples: { player: 'Feinberg', time: '6:40.123', window: 'daily' }
    }
  ],
  settings: [
    {
      key: 'account',
      label: 'Linked Minecraft account',
      type: 'text',
      placeholder: 'Your Minecraft username',
      help: 'Default player for every command. Leave blank to use your Twitch username.'
    }
  ]
};
