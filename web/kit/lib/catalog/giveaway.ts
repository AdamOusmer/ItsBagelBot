// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS } from './giveaway-limits';
import { replyTokens, type ModuleDef } from './module-def';

export { GIVEAWAY_LIMITS } from './giveaway-limits';

export const giveawayRuleNumbers = [
  { key: 'min_points', max: GIVEAWAY_LIMITS.maxPoints },
  { key: 'min_watch_seconds', max: GIVEAWAY_LIMITS.maxWatchHours * 3600 },
  { key: 'min_follow_days', max: GIVEAWAY_LIMITS.maxFollowDays },
  { key: 'min_account_days', max: GIVEAWAY_LIMITS.maxAccountDays },
  { key: 'skip_winners_days', max: GIVEAWAY_LIMITS.maxSkipDays },
  { key: 'sub_luck_percent', max: GIVEAWAY_LIMITS.luckMax }
] as const;

export const giveawayRuleToggles = ['exclude_mods', 'followers_only'] as const;

export const GIVEAWAY_MODULE: ModuleDef = {
  id: 'giveaway',
  label: 'Giveaways',
  tagline: 'Draw item prizes for your viewers.',
  description: 'Open entries in chat, draw winners and track claims and prize delivery.',
  category: 'Play',
  href: '/giveaways',
  defaultEnabled: false,
  settings: [
    { key: 'defaultPrize', label: 'Default prize', type: 'text' },
    { key: 'winners', label: 'Default winners', type: 'number' },
    { key: 'windowSeconds', label: 'Entry window (seconds)', type: 'number' },
    { key: 'claimSeconds', label: 'Claim window (seconds)', type: 'number', help: 'Use 0 to turn off claims.' },
    { key: 'rerollNoShows', label: 'Automatically reroll no-shows', type: 'toggle' },
    { key: 'openedEnabled', label: 'Opening line', type: 'toggle', defaultOn: true },
    { key: 'wonEnabled', label: 'Winner line', type: 'toggle', defaultOn: true },
    { key: 'noShowEnabled', label: 'No-show line', type: 'toggle', defaultOn: true },
    { key: 'refusalEnabled', label: 'Entry refusal line', type: 'toggle', defaultOn: GIVEAWAY_LIMITS.refusalEnabled },
    { key: 'openedMessage', label: 'Opening message', type: 'textarea', placeholder: 'Giveaway: {giveaway:prize} | Winners: {giveaway:count} | Time: {giveaway:left}. Type !join.' },
    { key: 'wonMessage', label: 'Winner message', type: 'textarea', placeholder: '{giveaway:targets} won {giveaway:prize}!' },
    { key: 'defaultPreset', label: 'Default preset', type: 'select', hidden: true },
    { key: 'presets', label: 'Rule presets', type: 'textarea', hidden: true }
  ],
  replies: [
    {
      key: 'opened', label: 'Opening line', tagline: 'When entries open.', event: '!giveaway start',
      enableKey: 'openedEnabled', messageKey: 'openedMessage',
      defaultMessage: 'Giveaway: {giveaway:prize} | Winners: {giveaway:count} | Time: {giveaway:left}. Type !join.',
      tokens: replyTokens(['prize', 'count', 'left', 'mins', 'requirements'], {
        prize: 'a game key', count: '3', left: '3 min', mins: '3', requirements: 'Followers only'
      })
    },
    {
      key: 'won', label: 'Winner line', tagline: 'When the draw names its winners.', event: '!giveaway draw',
      enableKey: 'wonEnabled', messageKey: 'wonMessage',
      defaultMessage: '{giveaway:targets} won {giveaway:prize}!',
      tokens: replyTokens(['targets', 'prize', 'count', 'claim'], { targets: '@viewer', prize: 'a game key', count: '1', claim: '2 min' })
    }
  ],
  commands: [
    { trigger: '!giveaway start [preset] [prize]', summary: 'Open a giveaway with an optional preset and prize.', perm: 'mod' },
    { trigger: '!giveaway draw', summary: 'Close entries and draw winners.', perm: 'mod' },
    { trigger: '!giveaway cancel', summary: 'Cancel the open giveaway.', perm: 'mod' },
    { trigger: '!reroll [slot]', summary: 'Replace a candidate or reroll all no-shows.', perm: 'mod' },
    { trigger: '!join', summary: 'Enter the open giveaway.' },
    { trigger: '!check', summary: 'Check giveaway eligibility.' },
    { trigger: '!claim', summary: 'Claim a winning slot.' },
    { trigger: '!winner', summary: 'Show the latest giveaway winners.' }
  ]
};
