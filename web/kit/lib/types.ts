// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '@bagel/ui/lib/icons';
export type Perm = 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
export type Tier = 'premium' | 'standard';
export type Role = 'streamer' | 'mod';

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
  aliases?: string[];
  response: string;
  is_active: boolean;
  stream_online_only?: boolean;
  perm?: Perm;
  cooldown?: number;
  allowed_user_id?: string;
  bump_counter?: string;
  uses?: string;
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
  host?: string;
  session_id?: string;
  bound: boolean;
  handshake_in_flight?: boolean;
  keepalive_ms?: number;
  attempts?: number;
  load?: number;
  burst_load?: number;
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
  capacity?: IngressCapacity;
  trial_loads?: Record<string, number>;
  trial_burst_loads?: Record<string, number>;
  trial_sockets?: TrialSocket[];
}

export interface TrialSocket {
  slot: number;
  node: string;
  state: 'connected' | 'connecting' | 'idle';
  channels: number;
  load: number;
  burst?: number;
}

export interface IngressCapacity {
  benchmark: string;
  nats_benchmark: string;
  load_window_seconds: number;
  burst_window_seconds?: number;
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

export interface NavChild {
  href: string;
  label: string;
  count?: string | number;
}

export interface NavLink {
  href: string;
  icon?: IconName;
  label: string;
  active?: boolean;
  locked?: boolean;
  count?: string | number;
  children?: NavChild[];
}

export interface NavGroupDef {
  label?: string;
  items: NavLink[];
}

export interface DashboardLink {
  href: string;
  name: string;
}

export * from './catalog/builtin-commands';
export * from './catalog/module-def';
export * from './catalog/index';
export * from './channelpoints';
export * from './timers';
export * from './loyalty';
export * from './govee';

export type {
  AutomodTerms,
  CollisionRef,
  ImportDiagnostic,
  ImportManifest,
  ImportSource,
  ImportStats,
  ManifestCommand,
  ManifestQuote,
  ManifestTimer,
  ManifestTrigger,
  PreviewResponse,
  CommitResponse
} from './importer/types';
export { IMPORT_SOURCES, IMPORT_ITEM_CAPS } from './importer/types';
