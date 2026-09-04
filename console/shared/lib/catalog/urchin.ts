// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';
import { BW_SESSION_SAMPLES, BW_SESSION_TOKENS } from './rehearsal-tokens';

export const URCHIN_MODULE: ModuleDef = 
// External-stats modules: chat commands answered through the gossip service
// (external API proxy + cache). Config keys must match the sesame module
// structs (app/twitch/sesame/modules/urchin.go, mcsr.go).
{
  id: 'urchin',
  label: 'Bedwars Stats',
  tagline: 'Hypixel Bedwars stats, urchin score and blacklist tags in chat.',
  description:
    'Viewer commands backed by urchin.gg: daily, weekly and monthly Bedwars sessions, lifetime stats, the Urchin sniper score and active blacklist tags. Commands default to your linked Minecraft account; viewers can also name any player, e.g. "!daily Technoblade".',
  icon: 'dirt',
  category: 'Stats',
  defaultEnabled: false,
  replies: [
    {
      key: 'daily',
      label: '!daily',
      tagline: 'Bedwars session since the daily reset.',
      event: '!daily',
      command: 'daily',
      enableKey: 'dailyEnabled',
      messageKey: 'dailyMessage',
      defaultMessage: '{player} today: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
      tokens: BW_SESSION_TOKENS,
      previewSamples: BW_SESSION_SAMPLES
    },
    {
      key: 'weekly',
      label: '!weekly',
      tagline: 'Bedwars session since the weekly reset.',
      event: '!weekly',
      command: 'weekly',
      enableKey: 'weeklyEnabled',
      messageKey: 'weeklyMessage',
      defaultMessage: '{player} this week: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
      tokens: BW_SESSION_TOKENS,
      previewSamples: BW_SESSION_SAMPLES
    },
    {
      key: 'monthly',
      label: '!monthly',
      tagline: 'Bedwars session since the monthly reset.',
      event: '!monthly',
      command: 'monthly',
      enableKey: 'monthlyEnabled',
      messageKey: 'monthlyMessage',
      defaultMessage: '{player} this month: {wins}W {losses}L · {finals} finals · {beds} beds · {fkdr} FKDR',
      tokens: BW_SESSION_TOKENS,
      previewSamples: BW_SESSION_SAMPLES
    },
    {
      key: 'stats',
      label: '!bwstats',
      tagline: 'Lifetime Bedwars stats.',
      event: '!bwstats',
      command: 'bwstats',
      enableKey: 'statsEnabled',
      messageKey: 'statsMessage',
      defaultMessage: '{player}: {stars} stars · {wins} wins · {finals} finals · {fkdr} FKDR · {beds} beds broken',
      tokens: ['player', 'stars', 'wins', 'losses', 'finals', 'finaldeaths', 'beds', 'fkdr', 'wlr'],
      previewSamples: {
        player: 'Technoblade',
        stars: '402',
        wins: '1000',
        losses: '100',
        finals: '5000',
        finaldeaths: '500',
        beds: '2000',
        fkdr: '10.00',
        wlr: '10.00'
      }
    },
    {
      key: 'sniper',
      label: '!sniper',
      tagline: 'Urchin (Cubelify overlay) score.',
      event: '!sniper',
      command: 'sniper',
      enableKey: 'sniperEnabled',
      messageKey: 'sniperMessage',
      defaultMessage: '{player} urchin score: {score}',
      tokens: ['player', 'score', 'mode', 'tagcount'],
      previewSamples: { player: 'Technoblade', score: '7.5', mode: 'warn', tagcount: '1' }
    },
    {
      key: 'tags',
      label: '!tag',
      tagline: 'Active Urchin blacklist tags.',
      event: '!tag',
      command: 'tag',
      enableKey: 'tagsEnabled',
      messageKey: 'tagsMessage',
      defaultMessage: '{player}: {tags}',
      tokens: ['player', 'tags', 'tagcount'],
      previewSamples: {
        player: 'Technoblade',
        tags: 'Blatant Cheater (added Jul 3, 2024)',
        tagcount: '1'
      }
    },
    {
      key: 'tagdescription',
      label: '!tagdescription',
      tagline: 'Blacklist tags with their reasons.',
      event: '!tagdescription',
      command: 'tagdescription',
      enableKey: 'tagDescriptionEnabled',
      messageKey: 'tagDescriptionMessage',
      defaultMessage: '{player}: {tags}',
      tokens: ['player', 'tags', 'tagcount'],
      previewSamples: {
        player: 'Technoblade',
        tags: 'Blatant Cheater (bhop - added Jul 3, 2024)',
        tagcount: '1'
      }
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
