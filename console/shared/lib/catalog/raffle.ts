// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const RAFFLE_MODULE: ModuleDef = 
{
  id: 'raffle',
  label: 'Raffle',
  tagline: 'Timed random draws your chat enters with !join.',
  description:
    'Open a raffle and viewers type !join to enter. While it runs the bot posts a time-left reminder every few minutes (the cadence is yours to set), and when time runs out it draws automatically — winners are picked uniformly at random from everyone who entered, every entry counts once, and the draw leaves a verifiable receipt behind. Winners confirm with !claim inside a 15-minute window. You (and your mods) also control everything from chat: !raffle open starts one, !raffle draw closes early and announces, !raffle cancel tears it down without drawing. When both this and the Play Queue are on, !join belongs to the raffle and the queue is reachable through !queue join.',
  icon: 'gem',
  category: 'Community',
  defaultEnabled: false,
  // The viewer-facing conversational replies are customizable per broadcaster
  // and each rehearses as its command (a viewer types the trigger, the bot
  // answers) with this reply's own sample values. The status readout, mod
  // confirmations and claim outcomes beyond the first stay fixed system text
  // (see app/sesame/modules/raffle.go); so do the engine-posted auto-close
  // and reminder announcements.
  replies: [
    {
      key: 'joined',
      label: 'Entry confirmation',
      tagline: 'When a viewer joins the raffle.',
      event: '!join',
      command: 'join',
      messageKey: 'joinMessage',
      defaultMessage: "@{user} you're in! {count} entered so far. Good luck!",
      tokens: ['user', 'count'],
      previewSamples: { user: 'sesame_sam', count: '12' }
    },
    {
      key: 'already',
      label: 'Already entered',
      tagline: 'When a viewer who is already in types !join again.',
      event: '!join',
      command: 'join',
      messageKey: 'alreadyMessage',
      defaultMessage: '@{user} you are already in this raffle ({count} entered).',
      tokens: ['user', 'count'],
      previewSamples: { user: 'sesame_sam', count: '13' }
    },
    {
      key: 'noRaffle',
      label: 'No raffle running',
      tagline: "When someone joins while no raffle is open.",
      event: '!join',
      command: 'join',
      messageKey: 'noRaffleMessage',
      defaultMessage: '@{user} no raffle is running right now.',
      tokens: ['user'],
      previewSamples: { user: 'sesame_sam' }
    },
    {
      key: 'opened',
      label: 'Raffle opened',
      tagline: 'When a raffle starts and entries open.',
      event: '!raffle open',
      command: 'raffle open',
      messageKey: 'openedMessage',
      defaultMessage: 'Raffle is LIVE! Type !join to enter — drawing in {mins} min!',
      tokens: ['mins'],
      previewSamples: { mins: '10' }
    },
    {
      key: 'won',
      label: 'Winners announced',
      tagline: 'When a draw names its winners (manual or timed).',
      event: '!raffle draw',
      command: 'raffle draw',
      messageKey: 'wonMessage',
      defaultMessage:
        '{targets} — congratulations! You won the raffle ({count} winner(s) from {entrants})! Type !claim within {claim} min to confirm your prize!',
      tokens: ['targets', 'count', 'entrants', 'claim'],
      previewSamples: { targets: '@maya_live, @crustycrumbs', count: '2', entrants: '18', claim: '15' }
    },
    {
      key: 'claimOk',
      label: 'Prize confirmed',
      tagline: 'When a winner confirms with !claim.',
      event: '!claim',
      command: 'claim',
      messageKey: 'claimOkMessage',
      defaultMessage: '@{user} your prize is confirmed — enjoy!',
      tokens: ['user'],
      previewSamples: { user: 'maya_live' }
    }
  ],
  commands: [
    { trigger: '!winner', summary: "Recall the last draw's winners and how many confirmed." },
    { trigger: '!raffle', summary: 'Show whether a raffle is running, entries and time left.' },
    { trigger: '!raffle open [minutes] [winners] [remind]', summary: 'Start a raffle (defaults 10 min, 1 winner, reminders every 5 min; remind 0 turns reminders off).', perm: 'mod' },
    { trigger: '!raffle draw [winners]', summary: 'Close now and announce the winners.', perm: 'mod' },
    { trigger: '!raffle close', summary: 'Same as draw with the configured winner count.', perm: 'mod' },
    { trigger: '!raffle cancel', summary: 'Tear down without drawing.', perm: 'mod' }
  ]
};
