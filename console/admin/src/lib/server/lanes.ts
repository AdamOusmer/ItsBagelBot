// Lane (JetStream consumer) telemetry. The retired Go admin read this directly
// from the broker's JetStream management API (StreamsInfo -> ConsumersInfo) and
// kept aliases in a KV bucket — there is NO request/reply subject for it. The
// console's shared NATS client is a core request/reply helper and intentionally
// does not speak the JetStream management API, so we cannot port the live read
// here without a new RPC contract. Until one exists, the screen renders a
// graceful degraded state (sample rows under DEMO, an empty/degraded notice
// otherwise) rather than guessing wire formats.
import { isDemo } from './access';

export interface LaneView {
  stream: string;
  consumer: string;
  display: string;
  subject: string;
  category: string; // "system" | "projection" | "ephemeral"
  ephemeral: boolean;
  orphan: boolean;
  pending: number;
  inFlight: string;
  rate: string;
  redelivered: number;
}

const sampleLanes: LaneView[] = [
  { stream: 'TWITCH_OUTGRESS', consumer: 'chat-egress', display: 'chat egress', subject: 'twitch.outgress.system', category: 'system', ephemeral: false, orphan: false, pending: 0, inFlight: '0 / 256', rate: '18 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'projection-users', display: 'users projection', subject: 'bagel.data.users.>', category: 'projection', ephemeral: false, orphan: false, pending: 3, inFlight: '1', rate: '2.4 msg/s', redelivered: 0 },
  { stream: 'BAGEL_DATA', consumer: 'cache-invalidate-7f3a', display: 'ephemeral', subject: 'bagel.data.invalidate', category: 'ephemeral', ephemeral: true, orphan: true, pending: 0, inFlight: '0', rate: '—', redelivered: 0 }
];

export interface LanesResult {
  lanes: LaneView[];
  degraded: boolean;
  notice: string;
}

export function loadLanes(): LanesResult {
  if (isDemo()) {
    return { lanes: sampleLanes, degraded: false, notice: '' };
  }
  // No request/reply subject exists for JetStream consumer telemetry; the
  // console client cannot read it. Degrade rather than fabricate.
  return {
    lanes: [],
    degraded: true,
    notice:
      'Lane telemetry is read directly from the JetStream management API, which the console RPC client does not speak. Add an admin RPC contract for lane snapshots to enable this view.'
  };
}

// Lane mutations (alias/durable/delete) hit the JetStream management API too,
// so there is nothing to call. Surface an honest message.
export function laneMutationUnavailable(action: string): string {
  return `${action} is unavailable: no admin RPC subject exists for JetStream lane management. It must be added before the console can mutate lanes.`;
}
