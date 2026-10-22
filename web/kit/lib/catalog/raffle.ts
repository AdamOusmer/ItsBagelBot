// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const RAFFLE_MODULE: ModuleDef = 
{
  id: 'raffle',
  label: 'Raffle',
  tagline: 'Timed random draws your chat enters with !join.',
  description:
    'Open a raffle and viewers type !join to enter. When time runs out it draws automatically (raffles longer than the reminder gap also get a time-left reminder). Winners are picked uniformly at random from everyone who entered, every entry counts once, and the draw leaves a verifiable receipt behind. With Loyalty on, every draw pays its points prize (1,000 by default) into the loyalty balance of each winner. With Loyalty off, raffles run without points, and a custom prize through !claim still works. Each chat line can be switched off on its own: the per-viewer join confirmation is off by default, so entries stay quiet. !claim is optional and off by default; turn it on only for a custom giveaway prize (a game key, say), while points are always paid at the draw. Your mods run it from chat with the same commands StreamElements uses: !raffle [amount] [seconds] (or !sraffle) starts a single-winner raffle where one winner takes the whole amount; !mraffle splits the amount evenly across several winners, picked by entrant count (1 per 20 above 200 entries, 1 per 8 up to 200, 1 per 5 up to 50, 1 per 4 up to 20, half of them up to 10). Amounts take k and m (5k, 1.5k). !raffle draw closes early and announces, !cancelraffle (or !raffle cancel) tears it down without drawing, and viewers type !raffle for the status. When both this and the Play Queue are on, !join belongs to the raffle and the queue is reachable through !queue join.',
  category: 'Play',
  defaultEnabled: false,
  settings: [
    { key: 'prizePoints', label: 'Points prize', type: 'number', requires: 'loyalty', placeholder: '1000', help: 'Total points one draw pays when the command gives no amount, split evenly across its winners. 0 runs raffles without points.' },
    { key: 'durationSeconds', label: 'Raffle length', type: 'number', placeholder: '60', help: 'Seconds a raffle stays open when the command gives none. Minimum 5.' },
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
      event: '!raffle',
      command: 'raffle',
      enableKey: 'openedEnabled',
      messageKey: 'openedMessage',
      defaultMessage: 'Raffle is LIVE! Type !join to enter. Drawing in {mins} min!',
      tokens: replyTokens(['mins', 'secs'], { mins: '1', secs: '60' })
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
        'Congrats {targets}, you won the raffle! {amount} {points} just landed in your balance.',
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
    { trigger: '!raffle status', summary: 'Show whether a raffle is running, entries and time left (a viewer typing !raffle gets the same).' },
    { trigger: '!raffle [amount] [seconds]', summary: 'Start a single-winner raffle: one winner takes the whole amount (also !sraffle, !singleraffle). Amounts take k and m; defaults 1000 points, 60 seconds.', perm: 'mod' },
    { trigger: '!mraffle [amount] [seconds]', summary: 'Start a multi-winner raffle: the amount is split evenly, with winners set by entrant count at the draw (also !multiraffle).', perm: 'mod' },
    { trigger: '!raffle draw [winners]', summary: 'Close now and announce the winners.', perm: 'mod' },
    { trigger: '!raffle close', summary: 'Same as draw with the configured winner count.', perm: 'mod' },
    { trigger: '!raffle cancel', summary: 'Tear down without drawing.', perm: 'mod' },
    { trigger: '!cancelraffle', summary: 'Same as !raffle cancel.', perm: 'mod' }
  ]
};
