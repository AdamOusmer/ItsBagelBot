// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneView } from '$lib/server/lanes';
import type { StatusTone } from '@bagel/kit/status-tone';

export function laneKey(lane: LaneView): string {
  return `${lane.stream}/${lane.consumer}`;
}

export function laneTone(lane: LaneView): StatusTone {
  if (lane.orphan) return 'error';
  if (lane.ephemeral) return 'warning';
  return 'success';
}

export type LaneDraft = { alias: string };

export const ALIAS_MAX = 48;

export function normalizeAlias(raw: string): string {
  return raw.trim().slice(0, ALIAS_MAX);
}

export function currentAlias(lane: LaneView): string {
  if (lane.display === lane.consumer) return '';
  if (lane.display === 'ephemeral') return '';
  return lane.display;
}
