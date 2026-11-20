// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS as pins } from './giveaway-limits';
import { replyTokens, type ModuleDef } from './module-def';

export const RAFFLE_MODULE: ModuleDef = {
  id: 'raffle',
  label: 'Raffle',
  tagline: 'Timed random draws your chat enters with !join.',
  description: 'Run a points raffle with !join entries and a timed draw. Loyalty must be on to start. Winners split the prize evenly, rounded down to whole points.',
  category: 'Points',
  defaultEnabled: false,
  parent: 'loyalty',
  href: '/loyalty#games',
  settings: [
    { key: 'prizePoints', label: 'Points prize', type: 'number', placeholder: String(pins.raffleDefaultPrize), help: 'Total points split evenly across winners when no amount is given.' },
    { key: 'durationSeconds', label: 'Raffle length', type: 'number', placeholder: String(pins.raffleDefaultSeconds), help: 'Seconds a raffle stays open when the command gives none. Minimum 5.' },
    {
      key: 'bareRaffle', label: 'Default !raffle mode', type: 'select', placeholder: pins.raffleBareMode,
      help: 'Choose what !raffle starts when you use it without a subcommand.',
      options: [{ value: 'single', label: 'Single winner' }, { value: 'split', label: 'Split pot' }]
    },
    {
      key: 'midCountEnabled', label: 'Halfway entry count', type: 'toggle', defaultOn: pins.raffleMidCountEnabled,
      help: 'Post the entry count and time left halfway through the raffle.'
    },
    { key: 'remindEnabled', label: 'Time-left reminders', type: 'toggle', defaultOn: true, help: 'Posts the time-left reminder line while a raffle runs.' }
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
      event: '!raffle',
      command: 'raffle',
      enableKey: 'openedEnabled',
      messageKey: 'openedMessage',
      defaultMessage: 'Raffle is LIVE! 1 winner takes {raffle:amount} {raffle:points}. Type !join ({raffle:time}).',
      tokens: replyTokens(['mins', 'secs', 'amount', 'points', 'time'], { mins: '1', secs: '60', amount: '1000', points: 'points', time: '1 min' })
    },
    {
      key: 'won',
      label: 'Winners announced',
      tagline: 'When a draw names its winners (manual or timed).',
      event: '!raffle draw',
      command: 'raffle draw',
      enableKey: 'wonEnabled',
      messageKey: 'wonMessage',
      defaultMessage: '{raffle:targets} won {raffle:amount} {raffle:points}!',
      tokens: replyTokens(['targets', 'count', 'entrants', 'amount', 'points'], { targets: '@SnackPackPanda', count: '1', entrants: '18', amount: '1000', points: 'points' })
    }
  ],
  commands: [
    { trigger: '!join', summary: 'Enter the raffle while one is running (also !raffle join).' },
    { trigger: '!winner', summary: "Show the winners and payout status while the raffle settles." },
    { trigger: '!raffle status', summary: 'Show whether a raffle is running, entries and time left (a viewer typing !raffle gets the same).' },
    { trigger: '!raffle [amount] [seconds]', summary: 'Start a raffle in the configured mode. Use !sraffle for one winner. Amounts accept k and m.', perm: 'mod' },
    { trigger: '!mraffle [amount] [seconds] [winners]', summary: 'Start a raffle for 3 winners by default. Enter a winner count or use auto for one winner per 10 entries, rounded up.', perm: 'mod' },
    { trigger: '!raffle draw [winners]', summary: 'Close now and announce winners after payout. You can enter a winner count or auto.', perm: 'mod' },
    { trigger: '!raffle close', summary: 'Same as draw with the configured winner count.', perm: 'mod' },
    { trigger: '!raffle cancel', summary: 'Tear down without drawing.', perm: 'mod' },
    { trigger: '!cancelraffle', summary: 'Same as !raffle cancel.', perm: 'mod' }
  ]
};
