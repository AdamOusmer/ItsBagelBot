// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { LaneView } from '$lib/server/lanes';
import type { StatusTone } from '@bagel/shared/status-tone';

/**
 * A lane's identity. JetStream has no id for a consumer beyond its stream +
 * name pair, so this is the row key, the inspector's selection id and the two
 * fields every mutation posts -- one function so they cannot be built three
 * different ways and stop matching.
 */
export function laneKey(lane: LaneView): string {
  return `${lane.stream}/${lane.consumer}`;
}

/**
 * An orphan is a consumer nothing is bound to: it accrues pending messages that
 * no one will ever ack, so it is an error, not a warning. An ephemeral lane is
 * healthy but disappears on restart, which is a caution rather than a fault.
 */
export function laneTone(lane: LaneView): StatusTone {
  if (lane.orphan) return 'error';
  if (lane.ephemeral) return 'warning';
  return 'success';
}

/** The alias draft the inspector edits. `alias` empty means "no alias": the
 *  server falls the display name back to the consumer name. */
export type LaneDraft = { alias: string };

export const ALIAS_MAX = 48;

/** The alias as it will be posted: trimmed and capped exactly as ?/alias stores it. */
export function normalizeAlias(raw: string): string {
  return raw.trim().slice(0, ALIAS_MAX);
}

/**
 * The alias currently in force, or '' when the display name is just the
 * consumer name (or the placeholder an ephemeral lane gets). Seeding the field
 * with the fallback would make "clear the alias" look like a no-op edit.
 */
export function currentAlias(lane: LaneView): string {
  if (lane.display === lane.consumer) return '';
  if (lane.display === 'ephemeral') return '';
  return lane.display;
}
