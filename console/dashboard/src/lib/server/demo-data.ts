// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Every dashboard demo fixture, in one module (mirrors admin's demo-data.ts).
//
// Nothing here may ever reach a production build. The rule that makes that
// true: this module is ONLY ever pulled in through a dynamic import() sitting
// inside a branch guarded by SvelteKit's build-time `dev` constant. Rollup
// erases the branch when dev === false, the import edge goes with it, and the
// module never enters the production graph. A static import would defeat that
// (the sentinel below is a side effect, so the module could not be shaken out).
//
// The sentinel is the backstop: if a future edit does drag this module into a
// production build, the throw makes it fail loudly at import, and
// scripts/assert-production-clean.ts fails the build before that can ship.
import { dev } from '$app/environment';
import {
  MODULE_CATALOG,
  PERM_LABELS,
  blankReward,
  blankSpotifyRedeem,
  blankSpotifySr,
  blankTimer,
  type ChannelPointReward,
  type CommandView,
  type CounterDef,
  type CounterEntryView,
  type LoyaltyStanding,
  type TimerDef
} from '@bagel/shared';
import { DEFAULT_LOCALE } from '@bagel/shared/i18n';
import type { Session } from './session';
import type { AccountState, BillingState, NotificationWire } from './services';
import type { QuoteView } from './quotes-store';
import type { GoveeDevice, GoveeView } from './govee-store';
import type { FetchDefView, FetchKeyView } from './fetches-store';
import type { PublicStats } from './public-stats';
import type { PublicBoards } from './public-boards';
import type { CommandDigest, ConnData, ModuleDigest, ShareDigest } from '../../routes/(app)/+page.server';

if (!dev) throw new Error('DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

// The board id every fixture hangs off. Not a valid Twitch user id (the public
// profile route's own id check would reject it), so it can never collide with
// a real account's data even if it leaked into a live read.
export const DEMO_BOARD_ID = 'demo';

// Demo session lets the app render without the Twitch OAuth flow wired up.
export function demoSession(): Session {
  const now = Math.floor(Date.now() / 1000);
  return {
    user_id: DEMO_BOARD_ID,
    login: 'itsmavey',
    display_name: 'Mavey',
    role: 'streamer',
    // Inert placeholder: DEMO sessions never touch guard.ts's revocation
    // check (the DEMO branch returns before it runs), so this never needs to
    // be a real per-mint id.
    sid: 'demo-session',
    iat: now,
    expires_at: now + 3600
  };
}

// Shared between the /notifications page load and the layout's bell peek so
// DEMO=1 shows the same rows in both places.
export const demoNotifications: NotificationWire[] = [
  {
    id: 2,
    scope: 'broadcast',
    title: 'Scheduled maintenance tonight',
    body: 'The bot will restart briefly around midnight UTC. Commands may pause for a few seconds.',
    level: 'warning',
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 2 * 3600e3).toISOString(),
    read: false
  },
  {
    id: 1,
    scope: 'direct',
    title: 'Welcome aboard',
    body: "Thanks for joining ItsBagelBot — let us know if you run into anything.",
    level: 'info',
    created_by_login: 'itsmavey',
    created_at: new Date(Date.now() - 26 * 3600e3).toISOString(),
    read: true
  }
];

export const demoAuthorizedDashboards = [
  { href: '/delegate/enter?owner=42', name: 'ferret_king' },
  { href: '/delegate/enter?owner=77', name: 'bagel_queen' }
];

export const demoAccountState = { active: true, status: 'vip', onboarded: true, creatorCode: null };

// Sample grants covering the full lifecycle (pending + consumed) so the
// settings page renders and is exercisable without OAuth + NATS.
export const demoDelegationGiven = [
  { token: 'demo-pending-token-1234', sections: ['commands', 'modules'], delegate_login: '', consumed: false },
  { token: 'demo-consumed-token-5678', sections: ['commands'], delegate_login: 'trusty_mod', consumed: true }
];

export const demoDelegationReceived = [{ owner_user_id: '42', owner_login: 'ferret_king', sections: ['commands'] }];

export const demoSavedLocale = DEFAULT_LOCALE;

// The billing demo is a JOURNEY (free -> checkout -> paid -> cancel-pending),
// not a static fixture, so unlike every other demo* export above this one is
// mutable module state: a plain `let`, flipped by the mutators below and read
// back by demoBilling(). It starts FREE on purpose, otherwise the purchase
// buttons on the billing page would have nothing left to demonstrate. It lives
// for the process's lifetime (a dev server run), same lifetime as every other
// in-memory demo store in this file.
let demoBillingState: BillingState = {
  active: false,
  status: 'free',
  expiresAt: null,
  source: '',
  subscriptionRef: null,
  cancelPending: false
};

export function demoBilling(): { account: BillingState; links: { cancelUrl: string } } {
  return {
    account: demoBillingState,
    links: { cancelUrl: 'https://example.tebex.io/account' }
  };
}

// Stands in for the Tebex webhook that would otherwise flip the account async.
// A 'monthly' purchase gets a subscriptionRef (there is a live subscription to
// manage/cancel); 'single' does not (it is a one-off month, nothing recurs).
// Both still expire 30 days out, mirroring one paid month either way.
export function demoCheckoutComplete(plan: 'monthly' | 'single'): void {
  demoBillingState = {
    active: true,
    status: 'paid',
    expiresAt: new Date(Date.now() + 30 * 864e5).toISOString(),
    source: 'tebex',
    subscriptionRef: plan === 'monthly' ? 'tbx-r-demo' : null,
    cancelPending: false
  };
}

// Mirrors what a real Tebex cancellation does: the plan keeps running until
// expiresAt, it just will not renew. Only meaningful once a checkout has run.
export function demoCancelPending(): void {
  demoBillingState = { ...demoBillingState, cancelPending: true };
}

// Replays the journey from the top. Exported (not just used internally) so a
// demo session can be reset without restarting the dev server.
export function demoBillingReset(): void {
  demoBillingState = {
    active: false,
    status: 'free',
    expiresAt: null,
    source: '',
    subscriptionRef: null,
    cancelPending: false
  };
}

export type DemoTransaction = {
  id: string;
  kind: 'premium' | 'gift';
  plan: 'monthly' | 'single';
  recipient: string | null;
  amount: number;
  currency: string;
  at: string;
};

// In-memory ledger, one entry per completed fake checkout. Nothing renders it
// yet: it exists so the demo journey leaves a real record behind, the same way
// a Tebex webhook would leave one in the transactions service, for a future
// history UI to read.
export const demoTransactions: DemoTransaction[] = [];

// Both plans price the same $7 per paid month in this demo (matches the
// billing page's displayed price); only the recurrence differs, and that is
// captured on the billing state itself, not here.
export function demoRecordTransaction(kind: 'premium' | 'gift', plan: 'monthly' | 'single', recipient: string | null): void {
  demoTransactions.push({
    id: `demo-tx-${demoTransactions.length + 1}`,
    kind,
    plan,
    recipient,
    amount: 7,
    currency: 'CAD',
    at: new Date().toISOString()
  });
}

// Demo book so the quotes tab renders without a live backend.
export function demoQuotes(): QuoteView[] {
  return [
    { number: 1, text: 'I meant to do that.', added_by: 'mod_amy', created_at: '2026-06-02T20:14:00Z' },
    { number: 3, text: 'The bagels are sentient and I welcome them.', added_by: 'mod_amy', created_at: '2026-06-19T02:41:00Z' },
    { number: 4, text: 'Never trust a ferret with a keyboard.', added_by: 'streamer', created_at: '2026-07-01T18:03:00Z' }
  ];
}

// Whole-payload helpers: the route's demo branch returns one of these, so the
// branch is a single line and the fixture shape lives beside the fixture.
export function demoQuotesView() {
  return { enabled: true, addPerm: 'mod' as const, editPerm: 'mod' as const, quotes: demoQuotes() };
}

export function demoTimersView() {
  return { enabled: true, timers: demoTimers() };
}

// Demo timers so the tab renders without a live backend.
export function demoTimers(): TimerDef[] {
  return [
    { ...blankTimer(), id: 'demo-1', message: 'Follow on socials: twitch.tv/yourchannel', intervalSeconds: 900 },
    { ...blankTimer(), id: 'demo-2', message: '!discord for the community server', intervalSeconds: 1800 }
  ];
}

export function demoStandings(): LoyaltyStanding[] {
  return [
    { viewerId: '1', viewerLogin: 'sesame_sam', viewerName: 'sesame_sam', points: 12400, watchSeconds: 90_000 },
    { viewerId: '2', viewerLogin: 'bagel_fan', viewerName: 'Bagel_Fan', points: 8300, watchSeconds: 64_800 }
  ];
}

export function demoCounters(): CounterDef[] {
  return [
    { name: 'deaths', scope: 'channel', value: 137 },
    { name: 'hugs', scope: 'viewer', value: 0 },
    { name: 'raids', scope: 'command', value: 0 },
    { name: 'redeems', scope: 'viewer_command', value: 0 }
  ];
}

// demoEntries mirrors what counter.entries returns for each demo counter, so
// the inspector drill-down works offline too.
export function demoEntries(name: string): CounterEntryView[] {
  if (name === 'raids') {
    return [
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'raid', value: 41 },
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'so', value: 12 }
    ];
  }
  return [
    { viewerId: '101', viewerLogin: 'sesame_sam', viewerName: 'Sesame_Sam', command: name === 'redeems' ? 'hydrate' : '', value: 23 },
    { viewerId: '102', viewerLogin: 'bagel_fan', viewerName: 'Bagel_Fan', command: name === 'redeems' ? 'hydrate' : '', value: 9 }
  ];
}

// Demo rewards so the channel points tab renders without a live backend.
export function demoRewards(): ChannelPointReward[] {
  return [
    { ...blankReward(), id: 'demo-1', title: 'Say hi', cost: 100, action: 'chat', message: '{user} says hi! 👋', onRedeem: 'fulfill' },
    {
      ...blankReward(),
      id: 'demo-2',
      title: 'Pick the next map',
      cost: 2500,
      isUserInputRequired: true,
      backgroundColor: '#1f69ff',
      action: 'chat',
      message: '{user} picked the next map: {input}',
      onRedeem: 'fulfill',
      maxPerStreamEnabled: true,
      maxPerStream: 1
    }
  ];
}

export function demoGoveeView(): GoveeView {
  return {
    enabled: true,
    keyPresent: true,
    // One light configured, one left open, so the deck shows both states.
    bindings: [
      {
        device: 'AB:CD:EF:12:34:56',
        sku: 'H6159',
        deviceName: 'Desk strip',
        onRedeem: 'fulfill',
        rewardId: 'demo-reward',
        reward: { rewardId: 'demo-reward', title: 'Colour the desk strip', cost: 500, color: '#9147ff', cooldown: 0 },
        allowOffline: false,
        allowOff: true,
        replyMessage: '@{user} set the lights to {color}!'
      }
    ]
  };
}

export function demoGoveeDevices(): GoveeDevice[] {
  return [
    { device: 'AB:CD:EF:12:34:56', sku: 'H6159', name: 'Desk strip', color: true },
    { device: '11:22:33:44:55:66', sku: 'H6072', name: 'Floor lamp', color: true },
    { device: '99:88:77:66:55:44', sku: 'H5081', name: 'Smart plug', color: false }
  ];
}

// demoSpotifyView seeds the songqueue page in demo mode: module on, both
// request paths on, and a bound reward so the bound-state row renders.
export function demoSpotifyView() {
  return {
    enabled: true,
    sr: { ...blankSpotifySr(), enabled: true },
    redeem: {
      ...blankSpotifyRedeem(),
      enabled: true,
      rewardId: 'demo-reward',
      replyMessage: '@{user} queued {track}!',
      reward: { rewardId: 'demo-reward', title: 'Play a song', cost: 500, color: '#1db954', cooldown: 0 }
    }
  };
}

export function demoFetches(): { defs: FetchDefView[]; keys: FetchKeyView[] } {
  return {
    defs: [
      {
        name: 'weather',
        url: 'https://api.weatherdemo.example/v1/current?city=london',
        json_path: ['forecast', 'current', 'temp_f'],
        is_active: true,
        key_label: 'weather_api'
      },
      {
        name: 'cat_fact',
        url: 'https://catfactdemo.example/fact',
        json_path: ['fact'],
        is_active: true,
        key_label: ''
      }
    ],
    // last4 only — the demo never fabricates key material either.
    keys: [{ label: 'weather_api', last4: '9f2c', created_at: '2026-01-01T00:00:00.000Z' }]
  };
}

// `sample` mirrors what gossip returns for a DryRun: the raw upstream body the
// builder turns into a clickable field tree. It is shaped to match the demo
// `weather` definition's json_path (forecast.current.temp_f) so clicking through
// the demo tree produces the same token the demo def already stores — a demo
// whose tree disagreed with its own fixtures would teach the wrong thing.
export function demoFetchTestRun(): { status: string; values: string[]; ms: number; sample: string } {
  return {
    status: 'ok',
    values: ['71.2'],
    ms: 214,
    sample: JSON.stringify(
      {
        forecast: { current: { temp_f: 71.2, temp_c: 21.8, condition: 'Cloudy' }, updated: '2026-01-01T00:00:00Z' },
        city: 'London',
        ok: true
      },
      null,
      2
    )
  };
}

// Sample rows use the STORED key format (no leading "!" — chat adds it), same
// as what the projector serves; the UI renders the "!" itself.
export const demoCommandRows: CommandView[] = [
  { name: 'dice', aliases: ['roll'], response: '{user} rolls the dice… {random:1-6}!', perm: 'everyone', cooldown: 5, uses: '412', is_active: true, stream_online_only: true },
  { name: 'socials', aliases: ['social', 'links'], response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: '288', is_active: true },
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: '1.2k', is_active: true },
  { name: 'so', response: 'Go show some love to twitch.tv/{target} — absolute legend', perm: 'mod', cooldown: 0, uses: '96', is_active: true },
  { name: 'discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: 60, uses: '203', is_active: true },
  { name: 'debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: '14', is_active: false },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, uses: '521', is_active: true },
  { name: 'deaths', response: '{channel} has died {counter:deaths} times. {choice:F,RIP,ouch}', perm: 'sub', cooldown: 15, uses: '177', is_active: true }
];

// Home-page digests. The digest shape is the route's own, so the fixture ships
// the rows and the caller folds them with its real digest() function.
export const demoDigestRows: CommandView[] = [
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', is_active: true, uses: '1.2k' },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', is_active: true, uses: '521' },
  { name: 'dice', response: '{user} rolls the dice… {random:1-6}!', is_active: true, uses: '412' },
  { name: 'socials', response: 'Follow along → twitch.tv/itsmavey', is_active: true, uses: '288' },
  { name: 'debug', response: 'node={node}', is_active: false, uses: '14' }
];

export function demoConn(connectionUiState: (s: ConnData['signals']) => ConnData['ui']): ConnData {
  const signals: ConnData['signals'] = { grant: true, active: true, status: 'vip', sub: 'ok' };
  return { signals, ui: connectionUiState(signals) };
}

export const demoModuleDigest: ModuleDigest = {
  on: 1,
  total: MODULE_CATALOG.filter((m) => !m.hidden).length,
  ok: true
};

export const demoShareDigest: ShareDigest = { people: 1, pending: 1, ok: true };

export function demoCommandDigest(digest: (rows: CommandView[]) => Omit<CommandDigest, 'ok'>): CommandDigest {
  return { ...digest(demoDigestRows), ok: true };
}

// Public channel profile (/user/[channel]).
export const demoCreatorCode = 'MAVEY10';

export const demoPublicCommands = [
  {
    trigger: '!bagel',
    aliases: ['!snack'],
    response: '{user} tosses a warm bagel to {target}. Toasty.',
    perm: PERM_LABELS.everyone,
    cooldown: 10,
    liveOnly: false,
    uses: '1.2k'
  },
  {
    trigger: '!socials',
    aliases: ['!links'],
    response: 'Follow along on Twitch and everywhere else.',
    perm: PERM_LABELS.everyone,
    cooldown: 30,
    liveOnly: false,
    uses: '288'
  }
];

export const demoPublicModules = [
  {
    id: 'clip',
    label: 'Clip',
    category: 'Built-in',
    tagline: 'Let viewers capture and share a recent stream moment.',
    commands: [{ label: '!clip', meta: 'clip the last moment' }],
    events: []
  }
];

// A synthetic, steadily-growing public snapshot so the stats page can be
// previewed without a fleet behind it.
const DEMO_EPOCH = Date.parse('2026-01-01T00:00:00Z');

export function demoStats(now: number): PublicStats {
  const secs = (now - DEMO_EPOCH) / 1000;
  const msgRate = 84 + 12 * Math.sin(now / 45_000);
  const eventRate = 137 + 18 * Math.sin(now / 60_000 + 1.7);
  return {
    messages_total: Math.floor(1_508_000_000 + secs * 84),
    events_total: Math.floor(2_430_000_000 + secs * 137),
    msg_rate: msgRate,
    event_rate: eventRate,
    degraded: false
  };
}

// The two public leaderboards. The ranking is fixed — it is a lifetime view —
// but the counts climb with the clock like demoStats does, so a demo shows the
// same thing the live page does: boards that tick with the stream rather than
// sitting still between reloads.
const DEMO_CHANNELS = [
  { id: '11', name: 'bagelwatch', messages: 41_882_301, events: 68_004_112, feeds: 9_144 },
  { id: '12', name: 'nightcrust', messages: 28_100_774, events: 45_930_615, feeds: 7_820 },
  { id: '13', name: 'sourdoughgg', messages: 19_774_003, events: 31_006_884, feeds: 6_401 },
  { id: '14', name: 'poppyseed_tv', messages: 12_440_918, events: 20_118_733, feeds: 4_990 },
  { id: '15', name: 'everythingbagel', messages: 8_902_551, events: 15_772_400, feeds: 3_318 },
  { id: '16', name: 'lox_and_key', messages: 6_113_204, events: 10_884_009, feeds: 2_744 },
  { id: '17', name: 'schmear_tactics', messages: 4_502_886, events: 7_990_115, feeds: 1_902 },
  { id: '18', name: 'toastmodern', messages: 3_118_440, events: 5_442_007, feeds: 1_388 },
  { id: '19', name: 'proofingroom', messages: 2_004_119, events: 3_871_552, feeds: 940 },
  { id: '20', name: 'crumbcam', messages: 1_442_007, events: 2_660_884, feeds: 611 }
];

export function demoBoards(now: number): PublicBoards {
  const secs = (now - DEMO_EPOCH) / 1000;
  // Each channel accrues at its own pace, highest first, so the demo board
  // moves without ever reordering itself.
  const grown = DEMO_CHANNELS.map((c, i) => {
    const rate = 6 - i * 0.5;
    return {
      ...c,
      messages: Math.floor(c.messages + secs * rate),
      events: Math.floor(c.events + secs * rate * 1.7),
      feeds: Math.floor(c.feeds + secs / (600 + i * 120))
    };
  });
  return {
    channels: grown.map(({ id, name, messages, events }) => ({ id, name, messages, events })),
    feed: {
      total: grown.reduce((sum, c) => sum + c.feeds, 0),
      ranked: 57,
      entries: grown.map(({ id, name, feeds }) => ({ id, name, count: feeds }))
    },
    degraded: false
  };
}
