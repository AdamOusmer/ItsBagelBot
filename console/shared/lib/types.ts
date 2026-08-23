// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from './icons';
import { MODULE_CATALOG, type ModuleDef } from './module-catalog';

// The built-in command + module catalogs live beside this barrel in
// ./module-catalog and flow through here unchanged.
export * from './module-catalog';

// Wire types mirroring the Go NATS RPC contracts (JSON over core NATS).
export type Perm = 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type Tier = 'premium' | 'standard';
export type Role = 'streamer' | 'mod';

// Ordered low -> high privilege; drives the access <select> in the dashboard.
export const PERMS: readonly Perm[] = ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'];
export const PERM_LABELS: Record<Perm, string> = {
  everyone: 'Everyone',
  sub: 'Subscribers',
  vip: 'VIPs',
  mod: 'Moderators',
  lead_mod: 'Lead moderators',
  broadcaster: 'Broadcaster'
};

export interface CommandView {
  name: string;
  // Alternate names the command also answers to in chat.
  aliases?: string[];
  response: string;
  is_active: boolean;
  stream_online_only?: boolean;
  perm?: Perm;
  // Cooldown in seconds; 0 or undefined means no cooldown.
  cooldown?: number;
  // Twitch id of the only user allowed to run the command; '' or undefined = unrestricted.
  allowed_user_id?: string;
  // Lifetime execution counter. The backend sends a number; older sample data
  // used human-formatted strings ('1.2k'), so both are accepted for display.
  uses?: number | string;
  // When true this is a built-in command: its behavior is baked into the bot,
  // it has no editable response, and its on/off state is stored in the modules
  // service (not the commands service). The dashboard renders it read-only with
  // a toggle + preview. See BUILTIN_COMMANDS.
  builtin?: boolean;
}


export interface AdminUser {
  user_id: string;
  username: string;
  display_name?: string;
  status?: string;
}

export interface UserStats {
  total_users: number;
  active_users: number;
  premium_users: number;
  vip_users: number;
  paid_users: number;
}

export interface Shard {
  shard_id: number;
  state: string;
  node: string;
  // Worker node (machine) name the shard runs on. Falls back to the host part
  // of `node` when unset (e.g. local dev without the downward-API env).
  host?: string;
  session_id?: string;
  bound: boolean;
  handshake_in_flight?: boolean;
  keepalive_ms?: number;
  attempts?: number;
  load?: number;
  // False when ingress is running this session without a registry entry for
  // it: the socket serves, but no converge pass can see or steer it. Optional
  // so a snapshot from an ingress older than the unmanaged-shard sweep still
  // renders (absent reads as "no claim", not as "unmanaged").
  managed?: boolean;
}

export interface ShardSnapshot {
  generated_at: string;
  reporter: string;
  nodes: string[];
  shard_count: number;
  conduit_manager?: { state: string; node: string; conduit_id?: string };
  shards: Shard[];
  desired_count: number;
  target: number;
  min_shards: number;
  max_shards?: number;
  autoscale: boolean;
  max_load?: number;
  max_load_shard_id?: number | null;
  // Optional during a rolling deployment so a new console can still render a
  // snapshot returned by an older ingress pod.
  capacity?: IngressCapacity;
}

export interface IngressCapacity {
  benchmark: string;
  nats_benchmark: string;
  load_window_seconds: number;
  target_utilization_pct: number;
  pod_rated_eps: number;
  pod_target_eps: number;
  fleet_nodes: number;
  fleet_rated_eps: number;
  fleet_target_eps: number;
  nats_rated_eps: number;
  nats_target_eps: number;
  effective_rated_eps: number;
  effective_target_eps: number;
  bottleneck: 'nats' | 'ingress_compute';
  websocket_rated_eps: number;
  websocket_target_eps: number;
  websocket_autoscale_max_shards: number;
}

export interface NavLink {
  href: string;
  icon: IconName;
  label: string;
  active?: boolean;
  locked?: boolean;
  count?: string | number;
}

export interface NavGroupDef {
  label?: string;
  items: NavLink[];
}

// A dashboard the signed-in user has been granted access to (a delegation
// received). Rendered in the topbar account menu as a quick-switch link into
// the owner's board via /delegate/enter.
export interface DashboardLink {
  // Full href to enter the board, e.g. /delegate/enter?owner=<id>.
  href: string;
  // Owner's Twitch login, shown as the row name + gradient-badge initial.
  name: string;
}


// GOVEE_COLOR_NAMES are the colour words a viewer may type in the Govee reward
// input. It mirrors the sesame colour parser's named palette
// (app/sesame/modules/color.go) so the dashboard prompt/help never advertises a
// name the bot would then refuse; viewers can always give a hex code instead.
export const GOVEE_COLOR_NAMES: readonly string[] = [
  'red',
  'orange',
  'yellow',
  'green',
  'lime',
  'teal',
  'cyan',
  'blue',
  'navy',
  'purple',
  'violet',
  'indigo',
  'pink',
  'magenta',
  'white',
  'warm',
  'gold'
];

// Govee module shapes, shared by the server store and the dashboard components.
// The module binds channel-points rewards to smart lights: a viewer redeems a
// reward, types a colour (or "off"), and the bot drives that reward's light.
// One reward per light. The Twitch reward is owned by outgress; the bindings
// live in the "govee" module blob and are read by sesame's govee module.
export type GoveeOnRedeem = 'fulfill' | 'cancel' | 'leave';

// GoveeDevice is one controllable light on the broadcaster's Govee account.
export interface GoveeDevice {
  device: string;
  sku: string;
  name: string;
  color: boolean;
}

// GoveeReward mirrors the Twitch reward settings the dashboard shows for a light.
export interface GoveeReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

// GoveeBinding ties one reward to one light plus the behaviour sesame reads.
export interface GoveeBinding {
  device: string;
  sku: string;
  deviceName: string;
  onRedeem: GoveeOnRedeem;
  rewardId: string;
  reward: GoveeReward | null;
  allowOffline: boolean;
  allowOff: boolean;
  replyMessage: string;
}

export function moduleDef(id: string): ModuleDef | undefined {
  return MODULE_CATALOG.find((m) => m.id === id);
}

// --- Channel points -------------------------------------------------------
// The Channel Points tab (its own dashboard section, NOT a module tile) lets a
// broadcaster create Twitch custom rewards (created under the bot's client id,
// styled natively on Twitch) and bind each one to a bot action that runs when a
// viewer redeems it. The Twitch-side reward is owned by outgress (broadcaster
// token); the action binding is stored in the hidden "channelpoints" module blob
// and read by sesame's channelpoints module.

// The bot action a redemption triggers. 'chat' posts the reward's message;
// 'none' manages the reward only, leaving just the resolution policy to act.
export type RewardActionKind = 'chat' | 'none';
// What to do with the redemption in Twitch's request queue after the action:
// mark it fulfilled, cancel (refund the points), or leave it for a human mod.
export type RewardOnRedeem = 'fulfill' | 'cancel' | 'leave';

export const REWARD_ACTIONS: readonly RewardActionKind[] = ['chat', 'none'];
export const REWARD_ON_REDEEM: readonly RewardOnRedeem[] = ['fulfill', 'cancel', 'leave'];

// One channel-points reward as the dashboard works with it: the Twitch reward
// fields plus the local action binding, merged into a single row.
export interface ChannelPointReward {
  // id is the Twitch-assigned custom reward id; empty only on an unsaved draft.
  id: string;
  title: string;
  cost: number;
  prompt: string;
  backgroundColor: string;
  isEnabled: boolean;
  isPaused: boolean;
  isUserInputRequired: boolean;
  // Limit controls ("claimable once and so on").
  maxPerStreamEnabled: boolean;
  maxPerStream: number;
  maxPerUserPerStreamEnabled: boolean;
  maxPerUserPerStream: number;
  globalCooldownEnabled: boolean;
  globalCooldownSeconds: number;
  // Local action binding (stored in the channelpoints module blob, read by sesame).
  action: RewardActionKind;
  message: string;
  onRedeem: RewardOnRedeem;
  // Loyalty hooks: counter names a loyalty counter bumped once per redemption
  // (the reward title keys a per-user+command counter's bucket); points, when
  // positive, awards that many loyalty points to the redeemer.
  counter: string;
  // counterScope is the scope to CREATE the counter with when it doesn't exist
  // yet, so a broadcaster can make the counter straight from the reward editor
  // instead of the Counters page. Ignored when counter is empty, or when the
  // counter already exists (create is idempotent — it never changes a stored
  // scope). Defaults to per user + reward, the scope a reward-linked counter
  // almost always wants.
  counterScope: CounterScope;
  points: number;
  // liveOnly gates the loyalty writes (counter bump + points award) to when
  // the broadcaster is live, so channel points redeemed offline can't farm
  // currency or inflate a counter. The chat reply always runs.
  liveOnly: boolean;
}

// blankReward is the default draft for the "new reward" form.
export function blankReward(): ChannelPointReward {
  return {
    id: '',
    title: '',
    cost: 100,
    prompt: '',
    backgroundColor: '',
    isEnabled: true,
    isPaused: false,
    isUserInputRequired: false,
    maxPerStreamEnabled: false,
    maxPerStream: 1,
    maxPerUserPerStreamEnabled: false,
    maxPerUserPerStream: 1,
    globalCooldownEnabled: false,
    globalCooldownSeconds: 60,
    action: 'chat',
    message: '',
    onRedeem: 'fulfill',
    counter: '',
    counterScope: 'viewer_command',
    points: 0,
    liveOnly: false
  };
}

// One repeating chat message: stream-only (armed on stream.online, stopped on
// stream.offline; see sesame's ValkeyTimerStore). No Twitch-side entity, so
// unlike ChannelPointReward there is nothing to CRUD but this blob's own id.
export interface TimerDef {
  // id is a dashboard-generated id; empty only on an unsaved draft.
  id: string;
  message: string;
  intervalSeconds: number;
  enabled: boolean;
}

// blankTimer is the default draft for the "new timer" form.
export function blankTimer(): TimerDef {
  return { id: '', message: '', intervalSeconds: 600, enabled: true };
}

// --- Loyalty ----------------------------------------------------------------
// The loyalty economy: viewers earn points from subs, resubs, gift subs,
// cheers and watch time (a 5-minute tick over everyone in chat while live).
// Rates live in the "loyalty" module blob; standings and counters live in the
// loyalty service (bagel.rpc.loyalty.*).

// LoyaltyConfig mirrors sesame's LoyaltyModuleConfig blob: 0 means "use the
// default", a negative value switches that source off.
export interface LoyaltyConfig {
  pointsName: string;
  subPoints: number;
  resubPoints: number;
  giftSubPoints: number;
  cheerPointsPer100: number;
  watchPointsPerTick: number;
}

// LOYALTY_DEFAULTS are the effective rates behind a zero value, mirrored from
// sesame so the form can show what "default" means.
export const LOYALTY_DEFAULTS: LoyaltyConfig = {
  pointsName: 'points',
  subPoints: 500,
  resubPoints: 500,
  giftSubPoints: 100,
  cheerPointsPer100: 50,
  watchPointsPerTick: 10
};

export function blankLoyaltyConfig(): LoyaltyConfig {
  return { pointsName: '', subPoints: 0, resubPoints: 0, giftSubPoints: 0, cheerPointsPer100: 0, watchPointsPerTick: 0 };
}

// The broadcaster-facing counter scopes. 'command' pools every viewer into
// one total per command/reward; 'viewer_command' keeps one value per viewer
// per command. (A fifth, admin-only 'bot' scope exists service-side and never
// surfaces in the dashboard.)
export type CounterScope = 'channel' | 'viewer' | 'command' | 'viewer_command';
export const COUNTER_SCOPES: readonly CounterScope[] = ['channel', 'viewer', 'command', 'viewer_command'];

// CounterDef is one counter definition as counter.list returns it; value is
// the channel-scope tally (entry scopes keep per-viewer values instead).
export interface CounterDef {
  name: string;
  scope: CounterScope;
  value: number;
}

// CounterEntryView is one stored bucket of an entry-scoped counter (the
// per-counter leaderboard row).
export interface CounterEntryView {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  command: string;
  value: number;
}

// LoyaltyStanding is one viewer's points + watch time (the channel top list).
export interface LoyaltyStanding {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  points: number;
  watchSeconds: number;
}

// --- Config importer -------------------------------------------------------
// The canonical import shapes live in lib/importer/types.ts since the
// standalone importer service was folded into the dashboard (2026-08-23) and
// that module became their single source of truth. Re-exported here so every
// existing '@bagel/shared' import keeps resolving unchanged. The NATS wire
// mirrors that existed solely for bagel.rpc.importer.* (PreviewRequest et al)
// were deleted with the protocol; PreviewResponse/CommitResponse survive as
// the dashboard action-result shapes.
export type {
  AutomodTerms,
  CollisionRef,
  ImportDiagnostic,
  ImportManifest,
  ImportSource,
  ImportStats,
  ManifestCommand,
  ManifestCounter,
  ManifestQuote,
  ManifestTimer,
  ManifestTrigger,
  PreviewResponse,
  CommitResponse
} from './importer/types';
export { IMPORT_SOURCES, IMPORT_ITEM_CAPS } from './importer/types';
