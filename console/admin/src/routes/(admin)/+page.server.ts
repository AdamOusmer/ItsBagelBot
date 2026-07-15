import type { PageServerLoad } from './$types';
import {
  shardSnapshot,
  userEnrollment,
  tokenStatus,
  auditList,
  type EnrollmentWire,
  type AuditEntry
} from '$lib/server/services';
import { isDemo, isManager } from '$lib/server/access';
import { sampleAudit, sampleEnrollment, sampleSnapshot } from '$lib/server/sample';
import { env } from '$env/dynamic/private';
import type { ShardSnapshot } from '@bagel/shared';

const AUDIT_PEEK = 6;

export type Overview = {
  enrollment: EnrollmentWire;
  snapshot: ShardSnapshot;
  botPresent: boolean;
  recentAudit: AuditEntry[];
  degraded: boolean;
};

// Resolve the independent reads in parallel rather than serial awaits, so the
// page waits one round trip instead of four. Every read falls back so the page
// keeps rendering when a responder is down; a failed *critical* read flips the
// degraded flag — the banner says so, the page never pretends the fallback is
// live. Fleet-wide RPC timing belongs to Analytics and is deliberately absent
// here, so a diagnostic timeout can never hold the operational overview on
// skeletons.
async function loadOverview(withAudit: boolean): Promise<Overview> {
  let degraded = false;
  const orFallback = <T>(load: Promise<T>, fallback: T, critical = true): Promise<T> =>
    load.catch(() => {
      degraded = degraded || critical;
      return fallback;
    });

  const botId = env.ADMIN_BOT_USER_ID ?? '';
  const [enrollment, snapshot, token, recentAudit] = await Promise.all([
    orFallback(userEnrollment(), sampleEnrollment),
    orFallback(shardSnapshot(), sampleSnapshot),
    orFallback(botId ? tokenStatus(botId) : Promise.resolve({ present: false }), { present: false }),
    orFallback(withAudit ? auditList(AUDIT_PEEK) : Promise.resolve([]), [])
  ]);

  return { enrollment, snapshot, botPresent: token.present, recentAudit, degraded };
}

export const load: PageServerLoad = async ({ parent }) => {
  const { role } = await parent();
  const withAudit = isManager(role);

  // Return the bundle as an unawaited promise so SvelteKit streams it: the page
  // shell renders immediately and the live data hydrates when the round trip
  // lands, instead of blocking SSR on NATS.
  const overview: Promise<Overview> = isDemo()
    ? Promise.resolve({
        enrollment: sampleEnrollment,
        snapshot: sampleSnapshot,
        botPresent: true,
        recentAudit: withAudit ? sampleAudit : [],
        degraded: false
      })
    : loadOverview(withAudit);

  return { overview, isManager: withAudit };
};
