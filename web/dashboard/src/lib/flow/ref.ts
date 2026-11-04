// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { ALERT_KEYS, type AlertKey } from '@bagel/kit/automations';

export type FlowRef =
  | { layer: 'automation'; id: string }
  | { layer: 'command'; name: string }
  | { layer: 'alert'; alert: AlertKey };

export const NEW_FLOW_ID = 'new';

const PREFIX = { automation: 'a-', command: 'c-', alert: 'l-' } as const;

export function flowRouteId(ref: FlowRef): string {
  if (ref.layer === 'automation') return PREFIX.automation + encodeURIComponent(ref.id);
  if (ref.layer === 'command') return PREFIX.command + encodeURIComponent(ref.name);
  return PREFIX.alert + ref.alert;
}

function decoded(text: string): string | null {
  try {
    return decodeURIComponent(text);
  } catch {
    return null;
  }
}

export function parseFlowRouteId(routeId: string): FlowRef | null {
  const rest = decoded(routeId.slice(2));
  if (!rest) return null;
  if (routeId.startsWith(PREFIX.automation)) return { layer: 'automation', id: rest };
  if (routeId.startsWith(PREFIX.command)) return { layer: 'command', name: rest };
  const alert = ALERT_KEYS.find((key) => key === rest);
  return routeId.startsWith(PREFIX.alert) && alert ? { layer: 'alert', alert } : null;
}
