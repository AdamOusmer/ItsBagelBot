// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneView } from '#lib/server/lanes';
import type { StatusTone } from '@bagel/kit/status-tone';

export function laneKey(lane: LaneView): string {
  return `${lane.stream}/${lane.consumer}`;
}

function laneHasWarning(lane: LaneView): boolean {
  if (lane.ephemeral) return true;
  if (lane.pending > 0) return true;
  return lane.redelivered > 0;
}

export function laneTone(lane: LaneView): StatusTone {
  if (lane.orphan) return 'danger';
  if (laneHasWarning(lane)) return 'warning';
  if (lane.connection === 'unknown') return 'neutral';
  return 'success';
}

export type LaneDraft = { alias: string };

export const ALIAS_MAX = 48;
const laneNameOrder = new Intl.Collator(undefined, { numeric: true });

export function normalizeAlias(raw: string): string {
  return raw.trim().slice(0, ALIAS_MAX);
}

export function currentAlias(lane: LaneView): string {
  if (lane.alias !== undefined) return lane.alias;
  if (lane.display === lane.consumer) return '';
  if (lane.display === 'ephemeral') return '';
  return lane.display;
}

export function renameLane(lane: LaneView, rawAlias: string): LaneView {
  const alias = normalizeAlias(rawAlias);
  const defaultDisplay = lane.defaultDisplay ?? (lane.ephemeral ? 'ephemeral' : lane.consumer);
  return { ...lane, alias, display: alias || defaultDisplay };
}

function compareLaneAttention(a: LaneView, b: LaneView): number {
  return Number(b.orphan) - Number(a.orphan) ||
    b.pending - a.pending ||
    b.redelivered - a.redelivered ||
    (b.ratePerSecond ?? 0) - (a.ratePerSecond ?? 0) ||
    laneNameOrder.compare(a.display, b.display) ||
    a.consumer.localeCompare(b.consumer);
}

export function groupLanes(lanes: LaneView[]): { stream: string; lanes: LaneView[]; pending: number }[] {
  const groups = new Map<string, LaneView[]>();
  for (const lane of lanes) {
    const group = groups.get(lane.stream) ?? [];
    group.push(lane);
    groups.set(lane.stream, group);
  }
  return [...groups].map(([stream, rows]) => ({
    stream,
    lanes: rows.sort(compareLaneAttention),
    pending: rows.reduce((sum, lane) => sum + lane.pending, 0)
  })).sort((a, b) => compareLaneAttention(a.lanes[0], b.lanes[0]) || a.stream.localeCompare(b.stream));
}
