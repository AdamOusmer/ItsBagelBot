// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from './icons';
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

// Compat barrel: everything that used to live in this god file now lives in
// the files below — catalog/ for the module catalog and built-in commands,
// govee/channelpoints/timers/loyalty for the page domain models. This file
// remains only so existing consumers keep importing from '@bagel/shared'
// unchanged (`export * from './types'` in index.ts); new code should import
// from the specific modules instead. The BW/FN preview token palettes moved to
// catalog/rehearsal-tokens.ts and are deliberately NOT re-exported here: they were
// module-private before and stay that way.
export * from './catalog/builtin-commands';
export * from './catalog/module-def';
export * from './catalog/index';
export * from './channelpoints';
export * from './timers';
export * from './loyalty';
export * from './govee';

// --- Config importer -------------------------------------------------------
// The canonical import shapes live in lib/importer/types.ts since the
// standalone importer service was folded into the dashboard (2026-08-23) and
// that module became their single source of truth. Re-exported here so every
// existing '@bagel/shared' import keeps resolving unchanged.
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
