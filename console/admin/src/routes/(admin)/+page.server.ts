import type { PageServerLoad } from './$types';
import {
  shardSnapshot,
  userEnrollment,
  tokenStatus,
  auditList,
  serviceHealth,
  type EnrollmentWire,
  type AuditEntry,
  type ServiceHealth
} from '$lib/server/services';
import { isDemo, isManager } from '$lib/server/access';
import { sampleAudit, sampleEnrollment, sampleHealth, sampleSnapshot } from '$lib/server/sample';
import { env } from '$env/dynamic/private';
import type { ShardSnapshot } from '@bagel/shared';

const AUDIT_PEEK = 6;

export type Overview = {
  enrollment: EnrollmentWire;
  snapshot: ShardSnapshot;
  botPresent: boolean;
  recentAudit: AuditEntry[];
  health: ServiceHealth[];
  degraded: boolean;
};

// Resolve the independent reads in parallel rather than serial awaits, so the
// page waits one round trip instead of four. allSettled keeps the page
// rendering even if one responder is slow or down; each failure flips the
// degraded flag and falls back to the last-known/sample value — the banner
// says so, the page never pretends the fallback is live.
async function loadOverview(withAudit: boolean): Promise<Overview> {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  const [enrollment, snapshot, token, audit, health] = await Promise.allSettled([
    userEnrollment(),
    shardSnapshot(),
    botId ? tokenStatus(botId) : Promise.resolve({ present: false }),
    withAudit ? auditList(AUDIT_PEEK) : Promise.resolve([]),
    // Probe failures are the health panel's own signal, not a degraded page.
    serviceHealth()
  ]);

  const degraded =
    enrollment.status === 'rejected' ||
    snapshot.status === 'rejected' ||
    (botId !== '' && token.status === 'rejected') ||
    (withAudit && audit.status === 'rejected');

  return {
    enrollment: enrollment.status === 'fulfilled' ? enrollment.value : sampleEnrollment,
    snapshot: snapshot.status === 'fulfilled' ? snapshot.value : sampleSnapshot,
    botPresent: token.status === 'fulfilled' && token.value.present,
    recentAudit: audit.status === 'fulfilled' ? audit.value : [],
    health: health.status === 'fulfilled' ? health.value : [],
    degraded
  };
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
        health: sampleHealth,
        degraded: false
      })
    : loadOverview(withAudit);

  return { overview, isManager: withAudit };
};
