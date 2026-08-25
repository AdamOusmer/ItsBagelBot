// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The loyalty wager games' catalog definitions, split out of types.ts to keep
// that file's declaration count sane: these two are the longest entries in
// the MODULE_CATALOG (settings + customizable replies) and change together
// with the sesame modules they mirror (app/sesame/modules/gamble.go,
// duel.go — same config keys, same defaults). They nest under loyalty
// (`parent: 'loyalty'`): no index tile, no independent master switch, no
// second currency name. Odds and chat lines stay on /modules/[id].
import type { ModuleDef } from './types';

export const GAME_MODULE_DEFS: ModuleDef[] = [
  {
    id: 'gamble',
    label: 'Gamble',
    tagline: 'Let viewers wager their points on a roll with !gamble.',
    description:
      'Give your loyalty points a game: viewers type !gamble <amount> (or half/all of their standing) and the bot rolls 1-100. Landing inside your win chance pays the stake back plus its match; anything else takes it. Set the win odds, bet limits and per-viewer cooldown below, and customize the win/lose lines. Every payout and debit moves real loyalty points through the same ledger as !points. Turned on from the Loyalty page — it cannot run while the currency is off, and it uses the same currency name.',
    icon: 'dice',
    category: 'Points',
    defaultEnabled: false,
    parent: 'loyalty',
    // The numeric knobs are plain settings the generic page patches into the
    // module blob; sesame clamps them server-side (engine.ClampGambleSettings),
    // so an out-of-range save can never arm an unlimited machine. Currency
    // name lives on loyalty, not here: a second copy drifted from the ledger
    // word and let this module look like its own economy.
    settings: [
      { key: 'winPercent', label: 'Win chance %', type: 'number', placeholder: '50', help: 'A roll of this number or lower wins. 50 is a fair coin; 1-99 allowed.' },
      { key: 'minBet', label: 'Minimum bet', type: 'number', placeholder: '1' },
      { key: 'maxBet', label: 'Maximum bet', type: 'number', placeholder: '1000' },
      { key: 'cooldownSeconds', label: 'Cooldown (seconds)', type: 'number', placeholder: '10', help: 'Per viewer — one chatter gambling never blocks another.' }
    ],
    replies: [
      {
        key: 'won',
        label: 'Win line',
        tagline: 'When a roll lands inside the win chance.',
        event: '!gamble 100',
        command: 'gamble',
        previewArgs: '100',
        messageKey: 'winMessage',
        defaultMessage: '@{user} rolled {roll} (needed {chance} or less) and won {amount} {points} — now at {balance}!',
        tokens: ['user', 'roll', 'chance', 'amount', 'balance', 'points'],
        previewSamples: { user: 'sesame_sam', roll: '23', chance: '50', amount: '100', balance: '1340', points: 'points' }
      },
      {
        key: 'lost',
        label: 'Loss line',
        tagline: 'When a roll misses the win chance.',
        event: '!gamble 100',
        command: 'gamble',
        previewArgs: '100',
        messageKey: 'loseMessage',
        defaultMessage: '@{user} rolled {roll} (needed {chance} or less) and lost {amount} {points}. Now at {balance}.',
        tokens: ['user', 'roll', 'chance', 'amount', 'balance', 'points'],
        previewSamples: { user: 'sesame_sam', roll: '87', chance: '50', amount: '100', balance: '1140', points: 'points' }
      }
    ],
    commands: [
      { trigger: '!gamble <amount>', summary: 'Wager that many points on a roll.' },
      { trigger: '!gamble half|all', summary: 'Wager half of / the whole standing.' }
    ]
  },
  {
    id: 'duel',
    label: 'Duels',
    tagline: 'Viewer-vs-viewer point duels: pot free-for-alls and 1v1 challenges.',
    description:
      "Two ways to duel for points. A pot duel: someone types !duel <stake> and everyone has the window to add their own stake — when time runs out the bot draws one winner weighted by stake and they take the whole pot. Or a challenge: !duel <user> <stake> names an opponent who must type !duel accept before the window closes; equal stakes, a fair coin flip, winner takes both. Decline, cancellation and no-shows always refund every escrowed point, and every movement goes through the loyalty service's guarded spend — nobody can wager what they do not have. Turned on from the Loyalty page; it uses the same currency name and stays off while loyalty is off.",
    icon: 'swords',
    category: 'Points',
    defaultEnabled: false,
    parent: 'loyalty',
    settings: [
      { key: 'minStake', label: 'Minimum stake', type: 'number', placeholder: '1' },
      { key: 'maxStake', label: 'Maximum stake', type: 'number', placeholder: '1000' },
      { key: 'potSeconds', label: 'Pot window (seconds)', type: 'number', placeholder: '60', help: 'How long a pot duel accepts stakes.' },
      { key: 'challengeSeconds', label: 'Accept window (seconds)', type: 'number', placeholder: '120', help: 'How long the challenged party has to accept.' }
    ],
    replies: [
      {
        key: 'opened',
        label: 'Pot duel opened',
        tagline: 'When a pot duel starts and stakes open.',
        event: '!duel 100',
        command: 'duel',
        previewArgs: '100',
        messageKey: 'openedMessage',
        defaultMessage: 'Pot duel is LIVE! @{user} put up {stake} {points} — type !duel <amount> to join. Drawing in {secs}s!',
        tokens: ['user', 'stake', 'secs', 'points'],
        previewSamples: { user: 'sesame_sam', stake: '100', secs: '60', points: 'points' }
      },
      {
        key: 'joined',
        label: 'Join confirmation',
        tagline: 'When a viewer adds their stake to a running pot.',
        event: '!duel 250',
        command: 'duel',
        previewArgs: '250',
        messageKey: 'joinMessage',
        defaultMessage: "@{user} you're in with {stake}! {count} in the duel, {pot} {points} in the pot.",
        tokens: ['user', 'stake', 'count', 'pot', 'points'],
        previewSamples: { user: 'sesame_sam', stake: '250', count: '4', pot: '700', points: 'points' }
      },
      {
        key: 'challenge',
        label: 'Challenge sent',
        tagline: 'When a viewer challenges another to even stakes.',
        event: '!duel @maya_live 500',
        command: 'duel',
        previewArgs: '@maya_live 500',
        messageKey: 'challengeMessage',
        defaultMessage: '@{user} challenges @{target} for {stake} {points}! @{target}, type !duel accept within {secs}s — winner takes {pot}!',
        tokens: ['user', 'target', 'stake', 'pot', 'secs', 'points'],
        previewSamples: { user: 'sesame_sam', target: 'maya_live', stake: '500', pot: '1000', secs: '120', points: 'points' }
      },
      {
        key: 'won',
        label: 'Result line',
        tagline: 'When a challenge is accepted and settled.',
        event: '!duel accept',
        command: 'duel',
        previewArgs: 'accept',
        messageKey: 'wonMessage',
        defaultMessage: 'The blades fall — @{winner} defeats @{loser} and takes {pot} {points}!',
        tokens: ['winner', 'loser', 'pot', 'points'],
        previewSamples: { winner: 'maya_live', loser: 'sesame_sam', pot: '1000', points: 'points' }
      }
    ],
    commands: [
      { trigger: '!duel', summary: 'Show what runs: a pot (entrants, pool, seconds) or a pending challenge.' },
      { trigger: '!duel <stake>', summary: 'Open a pot duel — or join one that is running.' },
      { trigger: '!duel <user> <stake>', summary: 'Challenge someone to even stakes; winner takes both.' },
      { trigger: '!duel accept', summary: 'The challenged party matches the stake and settles it instantly.' },
      { trigger: '!duel decline', summary: 'Refuse a challenge; the opener is refunded.', perm: 'mod' },
      { trigger: '!duel cancel', summary: 'Tear the running duel down with full refunds (opener or mod).', perm: 'mod' }
    ]
  }
];
