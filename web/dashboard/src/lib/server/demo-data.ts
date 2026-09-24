// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import {
  MODULE_CATALOG,
  catalogIndexable,
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
} from '@bagel/kit';
import { DEFAULT_LOCALE } from '@bagel/kit/i18n';
import type { Session } from './session';
import type { AccountState, BillingState, NotificationWire } from './services';
import type { QuoteView } from './quotes-store';
import type { GoveeDevice, GoveeView } from './govee-store';
import type { FetchDefView, FetchKeyView } from './fetches-store';
import type { PublicStats } from './public-stats';
import type { PublicBoards } from './public-boards';
import type { CommandDigest, ConnData, ModuleDigest, ShareDigest } from '../../routes/(app)/+page.server';
import type {
  StreamMeta,
  StreamCounters,
  ChatVolume,
  ActivityFeed,
  ActivityRow,
  ActivityKind,
  AnsweredTonight
} from '$lib/overview-live';

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

export const demoAccountState: AccountState = {
  active: true,
  status: 'vip',
  onboarded: true,
  creatorCode: null,
  username: 'demo',
  displayName: 'Demo'
};

export const demoDelegationGiven = [
  { token: 'demo-pending-token-1234', sections: ['commands', 'modules'], delegate_login: '', consumed: false },
  { token: 'demo-consumed-token-5678', sections: ['commands'], delegate_login: 'trusty_mod', consumed: true }
];

export const demoDelegationReceived = [{ owner_user_id: '42', owner_login: 'ferret_king', sections: ['commands'] }];

export const demoSavedLocale = DEFAULT_LOCALE;

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

export function demoCancelPending(): void {
  demoBillingState = { ...demoBillingState, cancelPending: true };
}

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

export const demoTransactions: DemoTransaction[] = [];

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

export function demoQuotes(): QuoteView[] {
  return [
    { number: 1, text: 'I meant to do that.', added_by: 'mod_amy', created_at: '2026-06-02T20:14:00Z' },
    { number: 3, text: 'The bagels are sentient and I welcome them.', added_by: 'mod_amy', created_at: '2026-06-19T02:41:00Z' },
    { number: 4, text: 'Never trust a ferret with a keyboard.', added_by: 'streamer', created_at: '2026-07-01T18:03:00Z' }
  ];
}

export function demoQuotesView() {
  return { enabled: true, addPerm: 'mod' as const, editPerm: 'mod' as const, quotes: demoQuotes() };
}

export function demoTimersView() {
  return { enabled: true, timers: demoTimers() };
}

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

export function demoDiscordView() {
  return {
    enabled: true,
    twitchLogin: 'demo',
    guilds: demoDiscordGuilds(),
    truncated: false
  };
}

export function demoDiscordGuilds() {
  return [
    {
      guildId: '123456789012345678',
      name: 'Demo Bakery',
      iconUrl: '/logo.png',
      memberCount: 1284,
      botPresent: true,
      needsReauth: false,
      reauthUnknown: false,
      boundAtMs: Date.now() - 40 * 24 * 60 * 60 * 1000
    },
    {
      guildId: '987654321098765432',
      name: 'Crumb Lounge',
      iconUrl: '',
      memberCount: 212,
      botPresent: false,
      needsReauth: false,
      reauthUnknown: false,
      boundAtMs: Date.now() - 3 * 24 * 60 * 60 * 1000
    },
    {
      guildId: '246813579024681357',
      name: 'Sourdough Society',
      iconUrl: '',
      memberCount: 5417,
      botPresent: true,
      needsReauth: true,
      reauthUnknown: false,
      boundAtMs: Date.now() - 9 * 60 * 60 * 1000
    }
  ];
}

export function demoDiscordPicker() {
  return [
    { id: '123456789012345678', name: 'Demo Bakery', owner: true, icon: '', permissions: '8' },
    { id: '456789012345678901', name: 'Toast Club', owner: false, icon: '', permissions: '32' },
    { id: '567890123456789012', name: 'Sourdough Guild', owner: true, icon: '', permissions: '8' },
    { id: '456789012345678999', name: 'Someone Else Server', owner: false, icon: '', permissions: '3136' }
  ];
}

export function demoDiscordBlocked() {
  return ['567890123456789012'];
}

export function demoDiscordDroppedPins() {
  return ['mods'];
}

export function demoDiscordRefusedField() {
  return 'ticketPanelTitle' as const;
}

export function demoDiscordConfig() {
  return {
    version: 4,
    found: true,
    config: {
      guildId: '123456789012345678',
      twitchLogin: 'demo',

      liveChannelId: '234567890123456789',
      clipsChannelId: '345678901234567890',
      welcomeChannelId: '456789012345678901',
      voiceHubId: '678901234567890123',
      logChannelId: '998877665544332211',

      subsChannelId: '112233445566778899',
      subsCategoryId: '112233445566778800',
      vipChannelId: '223344556677889911',
      vipCategoryId: '223344556677889900',

      ticketChannelId: '887766554433221100',
      ticketCategoryId: '776655443322110099',
      ticketArchiveCategoryId: '776655443322110088',
      ticketLogChannelId: '',
      ticketStaffRoleIds: '789012345678901299,901234567890123456',
      ticketOpenLimit: '2',
      ticketTranscriptEnabled: 'on',
      ticketPanelTitle: 'Need a hand?',
      ticketPanelBody: 'Open a private ticket and a mod will pick it up.',
      ticketPanelColor: '#c47a3a',
      ticketPanelButton: 'Open a ticket',

      ownerRoleId: '789012345678901234',
      leadModRoleId: '789012345678901299',
      modsRoleId: '901234567890123456',
      vipRoleId: '789012345678901255',
      subscriberRoleId: '789012345678901266',
      regularsRoleId: '',
      memberRoleId: '789012345678901277',
      pinnedRoles: 'mods=901234567890123456',

      liveEnabled: 'on',
      clipsEnabled: 'on',
      welcomeEnabled: 'on',
      goodbyeEnabled: 'off',
      voiceEnabled: 'on',
      ticketsEnabled: 'on',
      logsEnabled: 'on',
      subscribersEnabled: 'on',
      levelsEnabled: 'on',
      linkGuardEnabled: 'off',
      autoRoleEnabled: 'on',

      categoryAllow: '',
      categoryDeny: 'Just Chatting',
      linkAllowList: ''
    }
  };
}

export function demoDiscordLayout() {
  return {
    channels: [
      { id: '234567890123456789', name: 'now-live', type: 0 },
      { id: '345678901234567890', name: 'clips', type: 0 },
      { id: '456789012345678901', name: 'welcome', type: 0 },
      { id: '567890123456789013', name: 'announcements', type: 5 },
      { id: '890123456789012345', name: 'chat', type: 0 },
      { id: '887766554433221100', name: 'support', type: 0 },
      { id: '998877665544332211', name: 'logs', type: 0 },
      { id: '112233445566778899', name: 'subs-lounge', type: 0 },
      { id: '223344556677889911', name: 'vip-lounge', type: 0 },
      { id: '678901234567890123', name: '+ Create voice', type: 2 }
    ],
    categories: [
      { id: '776655443322110099', name: 'Tickets', type: 4 },
      { id: '776655443322110088', name: 'Archive', type: 4 },
      { id: '112233445566778800', name: 'Subscribers', type: 4 },
      { id: '223344556677889900', name: 'VIP', type: 4 }
    ],
    roles: [
      { id: '789012345678901234', name: 'Owner', type: 0 },
      { id: '789012345678901299', name: 'Lead Mod', type: 0 },
      { id: '901234567890123456', name: 'Mods', type: 0 },
      { id: '789012345678901255', name: 'VIP', type: 0 },
      { id: '789012345678901266', name: 'Subscriber', type: 0 },
      { id: '789012345678901277', name: 'Member', type: 0 }
    ],
    guild: {
      id: '123456789012345678',
      name: 'Demo Bakery',
      iconUrl: '/logo.png',
      memberCount: 1284
    },
    needsReauth: false,
    botOnline: true,
    botSinceMs: 0,
    lastCloseCode: 0
  };
}

export function demoDiscordStatus() {
  return {
    online: true,
    sinceMs: Date.now() - 3 * 60 * 60 * 1000,
    sessionResumes: 2,
    guildPresent: true,
    guild: {
      id: '123456789012345678',
      name: 'Demo Bakery',
      iconUrl: '/logo.png',
      memberCount: 1284
    },
    needsReauth: false,
    lastCloseCode: 0,
    code: '' as const,
    error: ''
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
  { name: 'dice', aliases: ['roll'], response: '{user} rolls the dice… {random:1-6}!', perm: 'everyone', cooldown: 5, uses: 412, is_active: true, stream_online_only: true },
  { name: 'socials', aliases: ['social', 'links'], response: 'Follow along → twitch.tv/itsmavey · @itsmavey everywhere', perm: 'everyone', cooldown: 30, uses: 288, is_active: true },
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', perm: 'everyone', cooldown: 10, uses: 1200, is_active: true },
  { name: 'so', response: 'Go show some love to twitch.tv/{target}, absolute legend', perm: 'mod', cooldown: 0, uses: 96, is_active: true },
  { name: 'discord', response: 'Join the bakery → discord.gg/itsbagelbot', perm: 'everyone', cooldown: 60, uses: 203, is_active: true },
  { name: 'debug', response: 'node={node} replica={id} lag={ms}ms', perm: 'broadcaster', cooldown: 0, uses: 14, is_active: false },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', perm: 'everyone', cooldown: 5, uses: 521, is_active: true },
  { name: 'deaths', response: '{channel} has died {counter:deaths} times. {choice:F,RIP,ouch}', perm: 'sub', cooldown: 15, uses: 177, is_active: true }
];

export const demoDigestRows: CommandView[] = [
  { name: 'bagel', response: '{user} tosses a warm bagel to {target}. Toasty.', is_active: true, uses: 1200 },
  { name: 'lurk', response: '{user} fades into the shadows. Thanks for the lurk.', is_active: true, uses: 521 },
  { name: 'dice', response: '{user} rolls the dice… {random:1-6}!', is_active: true, uses: 412 },
  { name: 'socials', response: 'Follow along → twitch.tv/itsmavey', is_active: true, uses: 288 },
  { name: 'debug', response: 'node={node}', is_active: false, uses: 14 }
];

export function demoConn(connectionUiState: (s: ConnData['signals']) => ConnData['ui']): ConnData {
  const signals: ConnData['signals'] = { grant: true, active: true, status: 'vip', sub: 'ok' };
  return { signals, ui: connectionUiState(signals) };
}

export const demoModuleDigest: ModuleDigest = {
  on: 1,
  total: MODULE_CATALOG.filter((m) => catalogIndexable(m)).length,
  ok: true
};

export const demoShareDigest: ShareDigest = { people: 1, pending: 1, ok: true };

export function demoCommandDigest(digest: (rows: CommandView[]) => Omit<CommandDigest, 'ok'>): CommandDigest {
  return { ...digest(demoDigestRows), ok: true };
}

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

export function demoBoards(now: number): PublicBoards {
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
    channels: grown.map(({ id, name, messages, events }) => ({ id, name, messages, events })),
    feed: {
      total: grown.reduce((sum, c) => sum + c.feeds, 0),
      ranked: 57,
      entries: grown.map(({ id, name, feeds }) => ({ id, name, count: feeds }))
    },
    degraded: false
  };
}

export function demoStreamMeta(now: number): StreamMeta {
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
  messages: 9412,
  answered: 148,
  modActions: 7,
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

export function demoActivityFeed(now: number): ActivityFeed {
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
