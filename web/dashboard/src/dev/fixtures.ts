// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import {
  MOD,
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
} from '@bagel/kit';
import { TERMS_VERSION } from '@bagel/kit/consent';
import type { Session } from '#lib/server/session';
import type { AccountState, BillingState, DeviceGrantView, NotificationWire } from '#lib/server/services';
import type { QuoteView } from '#lib/server/quotes-store';
import type { BlockedViewer, TtsOverlayView, VoiceAllowance, VoiceSettings } from '#lib/server/services/voice';
import type { AssetView, FeedAction, FeedControlArgs, FeedControlState, OverlayView, PoweredSettings } from '#lib/server/services/overlays';
import type { GoveeDevice, GoveeView } from '#lib/server/govee-store';
import type { FetchDefView, FetchKeyView } from '#lib/server/fetches-store';
import type { PublicStats } from '#lib/server/public-stats';
import type { PublicBoards } from '#lib/server/public-boards';
import type { QueueView } from '#lib/queue-snapshot';
import type { SongQueuePublicView, SongRow } from '#lib/songqueue-snapshot';
import type { SongQueueDoc } from '@bagel/kit/server/songqueue-store';
import type {
  StreamMeta,
  StreamCounters,
  ChatVolume,
  ActivityFeed,
  ActivityRow,
  ActivityKind,
  AnsweredTonight
} from '#lib/overview-live';

if (!dev) throw new Error('DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

export const DEMO_BOARD_ID = 'demo';

export function demoSession(): Session {
  const now = Math.floor(Date.now() / 1000);
  return {
    user_id: DEMO_BOARD_ID,
    login: 'itsmavey',
    display_name: 'Mavey',
    role: 'streamer',
    sid: 'demo-session',
    iat: now,
    expires_at: now + 3600
  };
}

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
    body: "Thanks for joining ItsBagelBot, let us know if you run into anything.",
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

export const demoMailingAddress = 'Demo mailing address';

export const demoAccountState: AccountState = {
  active: true,
  status: 'vip',
  onboarded: true,
  alpha: true,
  termsVersion: TERMS_VERSION,
  marketingStatus: 'none',
  creatorCode: 'MAVEY10',
  username: 'demo',
  displayName: 'Demo'
};

export const demoDelegationGiven = [
  { token: 'demo-pending-token-1234', sections: ['commands', 'modules'], delegate_login: '', consumed: false },
  { token: 'demo-consumed-token-5678', sections: ['commands'], delegate_login: 'trusty_mod', consumed: true }
];

export const demoDeviceGrants: DeviceGrantView[] = [
  {
    grantId: 'demo-grant-1',
    deviceId: 'demo-device-1',
    deviceName: "Mavey's MacBook",
    platform: 'macos',
    createdAt: '2026-10-20T10:00:00Z',
    lastUsedAt: '2026-11-05T09:30:00Z',
    expiresAt: '2027-01-18T10:00:00Z'
  },
  {
    grantId: 'demo-grant-2',
    deviceId: 'demo-device-2',
    deviceName: 'Stream PC',
    platform: 'windows',
    createdAt: '2026-09-02T08:00:00Z',
    lastUsedAt: null,
    expiresAt: '2027-03-01T08:00:00Z'
  }
];


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

type DemoTransaction = {
  id: string;
  kind: 'premium' | 'gift';
  plan: 'monthly' | 'single';
  recipient: string | null;
  amount: number;
  currency: string;
  at: string;
};

const demoTransactions: DemoTransaction[] = [];

export function demoRecordTransaction(entry: Pick<DemoTransaction, 'kind' | 'plan' | 'recipient'>): void {
  demoTransactions.push({
    id: `demo-tx-${demoTransactions.length + 1}`,
    ...entry,
    amount: 7,
    currency: 'CAD',
    at: new Date().toISOString()
  });
}

export function demoQuotes(): QuoteView[] {
  return [
    { number: 1, text: 'I meant to do that.', added_by: 'mod_amy', created_at: '2026-06-02T20:14:00Z' },
    { number: 3, text: 'The bagels are sentient and I welcome them.', added_by: 'mod_amy', created_at: '2026-06-19T02:41:00Z' },
    { number: 4, text: 'Never trust a ferret with a keyboard.', added_by: 'streamer', created_at: '2026-07-01T18:03:00Z' }
  ];
}

function demoTimers(): TimerDef[] {
  return [
    { ...blankTimer(), id: 'demo-1', message: 'Follow on socials: twitch.tv/yourchannel', intervalSeconds: 900, minChatLines: 15, chatWindowMinutes: 10 },
  ];
}

export function demoCounters(): CounterDef[] {
  return [
    { name: 'deaths', scope: 'channel', value: '137' },
    { name: 'hugs', scope: 'viewer', value: '0' },
    { name: 'raids', scope: 'command', value: '0' },
    { name: 'redeems', scope: 'viewer_command', value: '0' }
  ];
}

export function demoEntries(name: string): CounterEntryView[] {
  if (name === 'raids') {
    return [
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'raid', value: '41' },
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'so', value: '12' }
    ];
  }
  return [
    { viewerId: '101', viewerLogin: 'sesame_sam', viewerName: 'Sesame_Sam', command: name === 'redeems' ? 'hydrate' : '', value: '23' },
    { viewerId: '102', viewerLogin: 'bagel_fan', viewerName: 'Bagel_Fan', command: name === 'redeems' ? 'hydrate' : '', value: '9' }
  ];
}

function demoRewards(): ChannelPointReward[] {
  return [
    { ...blankReward(), id: 'demo-1', title: 'Say hi', cost: 100, action: 'chat', message: '{user} says hi!', onRedeem: 'fulfill' },
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

function demoGoveeView(): GoveeView {
  return {
    enabled: true,
    keyPresent: true,
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
    ],
    groups: [
      {
        name: 'Whole room',
        lights: [
          { device: 'AB:CD:EF:12:34:56', sku: 'H6159', name: 'Desk strip' },
          { device: '11:22:33:44:55:66', sku: 'H6072', name: 'Floor lamp' }
        ],
        onRedeem: 'fulfill',
        rewardId: 'demo-group-reward',
        reward: { rewardId: 'demo-group-reward', title: 'Colour the whole room', cost: 1000, color: '#9147ff', cooldown: 0 },
        allowOffline: false,
        allowOff: false,
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

function demoSpotifyView() {
  return {
    enabled: true,
    disabledTriggers: '',
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
    keys: [{ label: 'weather_api', last4: '9f2c', created_at: '2026-01-01T00:00:00.000Z' }]
  };
}

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

export const demoCommandRows: CommandView[] = [
  { name: 'dice', aliases: ['roll'], response: '{user} rolls the dice… {random:1-6}!', perm: 'everyone', cooldown: 5, uses: '412', is_active: true, stream_online_only: true },
  { name: 'socials', aliases: ['social', 'links'], response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: '288', is_active: true },
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: '1200', is_active: true },
  { name: 'so', response: 'Go show some love to twitch.tv/{target}, absolute legend', perm: 'mod', cooldown: 0, uses: '96', is_active: true },
  { name: 'debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: '14', is_active: false },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, user_cooldown: 60, uses: '521', is_active: true },
  { name: 'deaths', response: '{channel} has died {counter:deaths} times. {choice:F,RIP,ouch}', perm: 'sub', cooldown: 15, uses: '177', is_active: true }
];

const DEMO_EPOCH = Date.parse('2026-01-01T00:00:00Z');

export function demoStats(): PublicStats {
  const now = Date.now();
  const secs = (now - DEMO_EPOCH) / 1000;
  const msgRate = 14;
  const eventRate = 25;
  return {
    messages_total: String(Math.floor(1_508_000_000 + secs * msgRate)),
    events_total: String(Math.floor(2_430_000_000 + secs * eventRate)),
    sampled_at: now,
    msg_rate: msgRate,
    event_rate: eventRate,
    msg_rate_now: msgRate,
    event_rate_now: eventRate,
    degraded: false
  };
}

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

export function demoBoards(): PublicBoards {
  const now = Date.now();
  const secs = (now - DEMO_EPOCH) / 1000;
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
    channels: grown.map(({ id, name, messages, events }) => ({ id, name, messages: String(messages), events: String(events) })),
    feed: {
      total: String(grown.reduce((sum, c) => sum + c.feeds, 0)),
      ranked: '57',
      entries: grown.map(({ id, name, feeds }) => ({ id, name, count: String(feeds) }))
    },
    degraded: false
  };
}

export function demoStreamMeta(): StreamMeta {
  const now = Date.now();
  return {
    live: true,
    known: true,
    title: 'Rain World: blind run, day three',
    gameName: 'Rain World',
    startedAt: new Date(now - 222 * 60_000).toISOString(),
    endedAt: null,
    viewers: 1284,
    peakViewers: 1610,
    lastDurationMin: 248,
    ok: true
  };
}

export const demoStreamCounters: StreamCounters = {
  messages: '9412',
  answered: '148',
  modActions: '7',
  ok: true
};

export function demoChatVolume(): ChatVolume {
  const buckets = [
    18, 26, 22, 39, 33, 48, 42, 61, 55, 74, 66, 81, 58, 92, 74, 108, 82, 90, 64,
    78, 96, 68, 88, 56, 74, 50, 66, 40, 56, 46, 42
  ];
  return {
    buckets,
    commandTicks: [3, 7, 9, 13, 15, 16, 20, 22, 25, 28],
    now: buckets[buckets.length - 1],
    peak: Math.max(...buckets),
    ok: true
  };
}

const DEMO_FEED: [ActivityKind, string, string][] = [
  ['command', '!bagel answered @novaburst', '41ms'],
  ['automod', 'timeout 10m @linkspam_99 · link', 'floor 0.94'],
  ['timer', 'socials posted to chat', 'every 25m'],
  ['command', '!deaths incremented → 14', '33ms'],
  ['reward', 'Bagel Rain redeemed by @kettle', '500 pts'],
  ['event', 'new follower @gremlin_dev · shoutout sent', 'ok'],
  ['queue', 'Mount Kimbie - Made to Stray queued by @vex', '#4'],
  ['command', '!uptime answered @mods', '12ms'],
  ['loyalty', '412 watchers earned 5 points', 'tick']
];

export function demoActivityFeed(): ActivityFeed {
  const now = Date.now();
  const rows: ActivityRow[] = DEMO_FEED.map(([kind, text, meta], i) => ({
    id: `demo-${i}`,
    kind,
    text,
    meta,
    at: new Date(now - i * 37_000).toISOString()
  }));
  return { rows, medianMs: 38, dropped: 0, ok: true };
}

export const demoAnsweredTonight: AnsweredTonight = {
  commands: [
    { name: '!bagel', count: 46 },
    { name: '!uptime', count: 31 },
    { name: '!song', count: 28 },
    { name: '!deaths', count: 22 },
    { name: '!socials', count: 12 }
  ],
  ok: true
};

const DEMO_ADJECTIVES = ['sleepy', 'crispy', 'salty', 'toasty', 'sneaky', 'fuzzy', 'cosmic', 'gentle', 'rusty', 'lucky'];
const DEMO_NOUNS = ['otter', 'waffle', 'pickle', 'falcon', 'muffin', 'badger', 'cricket', 'noodle'];

const demoCrowd: LoyaltyStanding[] = DEMO_NOUNS.flatMap((noun, row) =>
  DEMO_ADJECTIVES.map((adjective, col) => {
    const i = row * DEMO_ADJECTIVES.length + col;
    const points = Math.round(14_000 * 0.93 ** i) + (i % 7);
    const login = `${adjective}_${noun}${i % 5 === 0 ? i : ''}`;
    const name = i % 3 === 0 ? login : `${adjective[0].toUpperCase()}${adjective.slice(1)}${noun[0].toUpperCase()}${noun.slice(1)}`;
    return {
      viewerId: String(601 + i),
      viewerLogin: login,
      viewerName: i % 11 === 10 ? '' : name,
      points: String(points),
      watchSeconds: points * 14 + i * 37
    };
  })
);

export const demoCurrentLogins = new Map([['pretzel_prime', '505']]);

const demoCurrencyName = 'bagels';

export function demoBoardViewers(): LoyaltyStanding[] {
  return [...demoLeaderboardHead, ...demoCrowd];
}

const demoLeaderboardHead: LoyaltyStanding[] = [
  { viewerId: '501', viewerLogin: 'crumbcatcher', viewerName: 'CrumbCatcher', points: '48210', watchSeconds: 612_000 },
  { viewerId: '502', viewerLogin: 'toastedtomato', viewerName: 'ToastedTomato', points: '41377', watchSeconds: 548_400 },
  { viewerId: '503', viewerLogin: 'sesame_sam', viewerName: 'Sesame_Sam', points: '36904', watchSeconds: 471_900 },
  { viewerId: '504', viewerLogin: 'lox_and_load', viewerName: 'lox_and_load', points: '28850', watchSeconds: 390_600 },
  { viewerId: '505', viewerLogin: 'pixelpretzel', viewerName: 'PixelPretzel', points: '24512', watchSeconds: 335_100 },
  { viewerId: '506', viewerLogin: 'nightowl_nox', viewerName: 'NightOwl_Nox', points: '19800', watchSeconds: 288_000 },
  { viewerId: '507', viewerLogin: 'cream_cheese_kid', viewerName: 'cream_cheese_kid', points: '19800', watchSeconds: 241_200 },
  { viewerId: '508', viewerLogin: 'everythingbagel', viewerName: 'EverythingBagel', points: '15230', watchSeconds: 205_800 },
  { viewerId: '509', viewerLogin: 'mossy_mallow', viewerName: 'Mossy_Mallow', points: '11046', watchSeconds: 162_000 },
  { viewerId: '510', viewerLogin: 'quietquokka', viewerName: 'QuietQuokka', points: '7385', watchSeconds: 98_700 },
  { viewerId: '511', viewerLogin: 'rye_not', viewerName: 'rye_not', points: '3120', watchSeconds: 41_400 },
  { viewerId: '512', viewerLogin: 'lurkinglarry', viewerName: '', points: '940', watchSeconds: 9_300 }
];

const DEMO_QUEUE_STEP_MS = 4000;
const DEMO_QUEUE_HEAD_START_MS = 90_000;
const demoQueueLogins = [
  'crumbcatcher', 'toastedtomato', 'sesame_sam', 'lox_and_load', 'pixelpretzel', 'nightowl_nox',
  'cream_cheese_kid', 'everythingbagel', 'mossy_mallow', 'quietquokka', 'rye_not', 'lurkinglarry',
  'butterbean', 'poppyseed_pete', 'onion_ring', 'garlicgoblin', 'schmear_queen', 'wheatwizard',
  'salty_pretzel', 'honeyoat', 'cinnamon_swirl', 'plain_jane', 'jalapeno_jo', 'blueberry_bo'
];

function demoQueueSize(step: number): number {
  const phase = step % 12;
  return 3 + (phase < 6 ? phase : 12 - phase);
}

export function demoQueue(): QueueView {
  const step = Math.floor(Date.now() / DEMO_QUEUE_STEP_MS);
  const size = demoQueueSize(step);
  const entries = Array.from({ length: size }, (_, i) => {
    const joiner = step - size + 1 + i;
    return {
      login: demoQueueLogins[joiner % demoQueueLogins.length],
      joinedAt: joiner * DEMO_QUEUE_STEP_MS - DEMO_QUEUE_HEAD_START_MS
    };
  });
  return { open: step % 20 < 16, total: size, entries, degraded: false };
}

const demoSongs: readonly Omit<SongRow, 'joinedAt'>[] = [
  { title: 'Mr. Brightside', artists: 'The Killers', requester: 'crumbcatcher' },
  { title: 'Human', artists: 'The Killers', requester: 'toastedtomato' },
  { title: 'Somebody Told Me', artists: 'The Killers', requester: 'sesame_sam' },
  { title: 'Take On Me', artists: 'a-ha', requester: 'lox_and_load' },
  { title: 'Dreams', artists: 'Fleetwood Mac', requester: 'pixelpretzel' },
  { title: 'Electric Feel', artists: 'MGMT', requester: 'nightowl_nox' },
  { title: 'Midnight City', artists: 'M83', requester: 'cream_cheese_kid' },
  { title: 'Safe and Sound', artists: 'Capital Cities', requester: 'everythingbagel' }
];

export function demoSongQueue(): SongQueuePublicView {
  const step = Math.floor(Date.now() / DEMO_QUEUE_STEP_MS);
  const size = demoQueueSize(step) + 1;
  const [current, ...up] = Array.from({ length: size }, (_, i) => ({
    ...demoSongs[(step + i) % demoSongs.length],
    joinedAt: (step - size + 1 + i) * DEMO_QUEUE_STEP_MS - DEMO_QUEUE_HEAD_START_MS
  }));
  return { open: step % 20 < 16, current, up, total: up.length, degraded: false };
}

const demoVoiceState = {
  settings: { enabled: true, voice: 'Kore', max_seconds: 30 } as VoiceSettings,
  overlayRevision: 1,
  blocked: [{ viewer_id: '9001', viewer_login: 'spamlord', created_at: 1_764_000_000 }] as BlockedViewer[]
};

export function demoVoiceAllowance(): VoiceAllowance {
  return demoAccountState.status === 'free'
    ? { plan: 'free', period: '2026-11', limit_tokens: 0, used_tokens: 0, remaining_tokens: 0, resets_at: '2026-12-01T00:00:00Z', reason: 'no_plan' }
    : { plan: 'premium', period: '2026-11', limit_tokens: 90_000, used_tokens: 57_300, remaining_tokens: 32_700, resets_at: '2026-12-01T00:00:00Z', reason: 'ok' };
}

export function demoVoiceOverlay(): TtsOverlayView {
  return { settings: structuredClone(demoVoiceState.settings), configured: true, revision: demoVoiceState.overlayRevision };
}

export function demoVoiceSettingsSave(settings: VoiceSettings): TtsOverlayView {
  demoVoiceState.settings = { ...settings };
  demoVoiceState.overlayRevision += 1;
  return demoVoiceOverlay();
}

export function demoVoiceBlocks(): BlockedViewer[] {
  return structuredClone(demoVoiceState.blocked);
}

export function demoVoiceBlock(viewer: { viewer_id: string; viewer_login: string }): BlockedViewer[] {
  demoVoiceState.blocked.push({ ...viewer, created_at: Math.floor(Date.now() / 1000) });
  return demoVoiceBlocks();
}

export function demoVoiceUnblock({ viewerId }: { viewerId: string }): BlockedViewer[] {
  demoVoiceState.blocked = demoVoiceState.blocked.filter((viewer) => viewer.viewer_id !== viewerId);
  return demoVoiceBlocks();
}

const DEMO_POWERED_URL = 'https://overlay.itsbagelbot.com/o#demo-powered-link';
const DEMO_POWERED_ID = 1;

const demoPoweredState: PoweredSettings = {
  id: DEMO_POWERED_ID,
  config: { every_minutes: 15, show_seconds: 8, directions: ['right'], align: 'end' },
  revision: 1
};
let demoRotations = 0;

const demoPoweredLink = () => `${DEMO_POWERED_URL}-${demoRotations}`;

export function demoOverlayList(): OverlayView[] {
  return [{ id: DEMO_POWERED_ID, kind: 'system', template: 'powered', name: 'Powered by Bagel', revision: demoPoweredState.revision }];
}

export function demoPoweredView(): PoweredSettings {
  return structuredClone(demoPoweredState);
}

export function demoPoweredSave(config: PoweredSettings['config']): number {
  demoPoweredState.config = structuredClone(config);
  demoPoweredState.revision += 1;
  return demoPoweredState.revision;
}

export function demoOverlayRotate(): { id: number; url: string; revision: number } {
  demoRotations += 1;
  demoPoweredState.revision += 1;
  return { id: DEMO_POWERED_ID, url: demoPoweredLink(), revision: demoPoweredState.revision };
}

const demoAssetState: AssetView[] = [
  { asset_id: 'demo-logo', url: 'https://cdn.itsbagelbot.com/demo/logo.png', type: 'image', bytes: 20480, w: 512, h: 512, duration_ms: 0, sha256: 'demo', display_name: 'logo.png' },
  { asset_id: 'demo-chime', url: 'https://cdn.itsbagelbot.com/demo/chime.ogg', type: 'sound', bytes: 40960, w: 0, h: 0, duration_ms: 1200, sha256: 'demo', display_name: 'chime.ogg' }
];

export const demoAssets = (): AssetView[] => structuredClone(demoAssetState);

export function demoAssetDelete(id: string): void {
  const index = demoAssetState.findIndex((asset) => asset.asset_id === id);
  if (index >= 0) demoAssetState.splice(index, 1);
}

const demoFeedState: FeedControlState = {
  control: { paused: false, muted: false, skip: 0 },
  subathon: { running: false, remaining_seconds: 0 }
};

const demoFeedSetters: Partial<Record<FeedAction, (state: FeedControlState, args: FeedControlArgs) => void>> = {
  pause: ({ control }) => { control.paused = true; },
  resume: ({ control }) => { control.paused = false; },
  mute: ({ control }) => { control.muted = true; },
  unmute: ({ control }) => { control.muted = false; },
  skip: ({ control }) => { control.skip += 1; },
  subathon_add: ({ subathon }, args) => { subathon.remaining_seconds = (subathon.remaining_seconds ?? 0) + (args.seconds ?? 0); },
  subathon_pause: ({ subathon }) => { subathon.running = false; },
  subathon_resume: ({ subathon }) => { subathon.running = true; },
  subathon_reset: ({ subathon }) => { subathon.running = false; subathon.remaining_seconds = 0; }
};

export function demoFeedControl({ action, args }: { action: FeedAction; args: FeedControlArgs }): FeedControlState {
  demoFeedSetters[action]?.(demoFeedState, args);
  return structuredClone(demoFeedState);
}

export function demoModuleConfigs(): Record<string, Record<string, unknown>> {
  const { bindings, groups } = demoGoveeView();
  const { sr, redeem } = demoSpotifyView();
  return {
    [MOD.timers]: { timers: demoTimers() },
    [MOD.govee]: { bindings, groups },
    [MOD.songqueue]: { sr, redeem },
    [MOD.loyalty]: { pointsName: demoCurrencyName },
    [MOD.channelpoints]: { rewards: demoRewards() }
  };
}

export function demoSongQueueDoc(): SongQueueDoc {
  const now = Date.now();
  const [current, ...up] = demoSongs.slice(0, 3).map((song, i) => ({
    tid: `demo-${i + 1}`,
    title: song.title,
    artists: [song.artists],
    dur: 200_000,
    req_id: String(501 + i),
    req_name: song.requester,
    at: now - (3 - i) * 60_000
  }));
  return { current, up };
}
