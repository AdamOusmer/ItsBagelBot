// Lane (JetStream consumer) telemetry. The retired Go admin read this directly
// from the broker's JetStream management API; the console's shared NATS client
// is a core request/reply helper and cannot speak that API. The `users` service
// (which holds $JS.> permission) now answers a lane snapshot + mutation RPC
// under the admin-user prefix on the console's behalf, so this module is a thin
// request/reply wrapper. Under DEMO it serves sample rows; on any RPC failure it
// degrades to an honest empty/notice state rather than fabricating data.
import { rpc } from '@bagel/shared/server/nats';
import { env } from '$env/dynamic/private';
import { isDemo } from './access';

// Same prefix the user-admin RPCs ride; the console's `admin` NATS user already
// publishes this wildcard and the `users` service subscribes it.
const PREFIX = env.NATS_ADMIN_USER_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user';

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

interface LanesReply {
  lanes: LaneView[];
  error?: string;
}

export interface LaneMutationResult {
  ok: boolean;
  notice?: string;
  error?: string;
}

export async function loadLanes(): Promise<LanesResult> {
  if (isDemo()) {
    return { lanes: sampleLanes, degraded: false, notice: '' };
  }
  try {
    const reply = await rpc<LanesReply>(`${PREFIX}.lanes.get`, {}, 5000);
    if (reply.error) {
      return { lanes: [], degraded: true, notice: reply.error };
    }
    return { lanes: reply.lanes ?? [], degraded: false, notice: '' };
  } catch {
    return {
      lanes: [],
      degraded: true,
      notice: 'Lane telemetry is currently unreachable (the broker or the users service did not answer).'
    };
  }
}

export async function laneAlias(
  stream: string,
  consumer: string,
  alias: string
): Promise<LaneMutationResult> {
  return rpc<LaneMutationResult>(`${PREFIX}.lanes.alias`, { stream, consumer, alias }, 5000);
}

export async function laneDurable(stream: string, consumer: string): Promise<LaneMutationResult> {
  return rpc<LaneMutationResult>(`${PREFIX}.lanes.durable`, { stream, consumer }, 5000);
}

export async function laneDelete(stream: string, consumer: string): Promise<LaneMutationResult> {
  return rpc<LaneMutationResult>(`${PREFIX}.lanes.delete`, { stream, consumer }, 5000);
}
