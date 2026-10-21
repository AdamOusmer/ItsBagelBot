// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const RAFFLE_MODULE: ModuleDef = 
{
  id: 'raffle',
  label: 'Raffle',
  tagline: 'Timed random draws your chat enters with !join.',
  description:
    'Open a raffle and viewers type !join to enter. While it runs the bot posts a time-left reminder every few minutes (the cadence is yours to set), and when time runs out it draws automatically. Winners are picked uniformly at random from everyone who entered, every entry counts once, and the draw leaves a verifiable receipt behind. Every draw pays its points prize (1,000 by default, split evenly across the winners) into the loyalty balance of each winner, so Loyalty must be on; set the prize to 0 for a raffle without points. Each chat line can be switched off on its own: the per-viewer join confirmation is off by default, so entries stay quiet. !claim is optional and off by default; turn it on only for a custom giveaway prize (a game key, say), while points are always paid at the draw. You (and your mods) also control everything from chat: !raffle open starts one, !raffle draw closes early and announces, !raffle cancel tears it down without drawing. When both this and the Play Queue are on, !join belongs to the raffle and the queue is reachable through !queue join.',
  category: 'Play',
  defaultEnabled: false,
  settings: [
    { key: 'prizePoints', label: 'Points prize', type: 'number', placeholder: '1000', help: 'Total points one draw pays, split evenly across its winners. 0 runs raffles without points.' },
    { key: 'remindEnabled', label: 'Time-left reminders', type: 'toggle', defaultOn: true, help: 'Posts the time-left reminder line while a raffle runs.' },
    { key: 'claimEnabled', label: 'Enable !claim', type: 'toggle', help: 'Turns on !claim for a custom giveaway prize. Points are always paid at the draw, whatever this is set to.' },
    { key: 'claimPrize', label: 'Custom prize', type: 'text', placeholder: 'a game key', help: 'The prize name shown in the winners line, for a giveaway that is not points.' }
  ],
  replies: [
    {
      key: 'joined',
      label: 'Entry confirmation',
      tagline: 'When a viewer joins the raffle.',
      event: '!join',
      command: 'join',
      enableKey: 'joinEnabled',
      defaultOff: true,
      messageKey: 'joinMessage',
      defaultMessage: "@{user} you're in! {count} entered so far. Good luck!",
      tokens: replyTokens(['user', 'count'], { user: 'PrincessBarney', count: '12' })
    },
    {
      key: 'already',
      label: 'Already entered',
      tagline: 'When a viewer who is already in types !join again.',
      event: '!join',
      command: 'join',
      enableKey: 'alreadyEnabled',
      defaultOff: true,
      messageKey: 'alreadyMessage',
      defaultMessage: '@{user} you are already in this raffle ({count} entered).',
      tokens: replyTokens(['user', 'count'], { user: 'PrincessBarney', count: '13' })
    },
    {
      key: 'noRaffle',
      label: 'No raffle running',
      tagline: "When someone joins while no raffle is open.",
      event: '!join',
      command: 'join',
      enableKey: 'noRaffleEnabled',
      messageKey: 'noRaffleMessage',
      defaultMessage: '@{user} no raffle is running right now.',
      tokens: replyTokens(['user'], { user: 'PrincessBarney' })
    },
    {
      key: 'opened',
      label: 'Raffle opened',
      tagline: 'When a raffle starts and entries open.',
      event: '!raffle open',
      command: 'raffle open',
      enableKey: 'openedEnabled',
      messageKey: 'openedMessage',
      defaultMessage: 'Raffle is LIVE! Type !join to enter. Drawing in {mins} min!',
      tokens: replyTokens(['mins'], { mins: '10' })
    },
    {
      key: 'won',
      label: 'Winners announced',
      tagline: 'When a draw names its winners (manual or timed).',
      event: '!raffle draw',
      command: 'raffle draw',
      enableKey: 'wonEnabled',
      messageKey: 'wonMessage',
      defaultMessage:
        '{targets}, congratulations! You won the raffle ({count} winner(s) from {entrants}): {amount} {points} per winner, already in your balance!',
      tokens: replyTokens(['targets', 'count', 'entrants', 'amount', 'points', 'claim', 'prize'], { targets: '@SnackPackPanda, @crustycrumbs', count: '2', entrants: '18', amount: '500', points: 'points', claim: '15', prize: 'a game key' })
    },
    {
      key: 'claimOk',
      label: 'Prize confirmed',
      tagline: 'When a winner confirms with !claim.',
      event: '!claim',
      command: 'claim',
      enableKey: 'claimOkEnabled',
      messageKey: 'claimOkMessage',
      defaultMessage: '@{user} your prize is confirmed. Enjoy!',
      tokens: replyTokens(['user'], { user: 'SnackPackPanda' })
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
