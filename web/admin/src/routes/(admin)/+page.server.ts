// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
import { dev } from '$app/environment';
import { allows } from '$lib/server/access';
import { bestEffort } from '@bagel/shared/server/best-effort';
import { emptyEnrollment, emptyShardSnapshot } from '$lib/server/fallback';
import { env } from '$env/dynamic/private';
import type { ShardSnapshot } from '@bagel/shared';
// The /analytics route folded into this page, and its one distinguishing
// control was the enrollment window. It lives in the URL rather than in
// component state so an operator can link a colleague at the 90-day view, and
// so the users service's per-window cache key is hit by a real navigation
// instead of a client refetch.
import { parseEnrollmentWindow, type EnrollmentWindow } from '$lib/enrollment-window';

const AUDIT_PEEK = 6;
const DEMO = dev && process.env.DEMO === '1';

// Each panel resolves independently and says whether its read landed, so the
// page renders a neutral empty shape WITHOUT presenting it as live data. The
// previous shape resolved all four reads into one bundle behind a single
// `degraded` flag, which meant a blipped audit peek made the shard panel claim
// it was degraded too. Fleet-wide RPC timing belongs to the health probe below
// and is deliberately NOT part of the enrollment read, so a diagnostic timeout
// can never hold the operational overview on skeletons.
export type Panel<T> = { value: T; ok: boolean };

function panel<T>(read: Promise<T>, fallback: T): Promise<Panel<T>> {
  return bestEffort(
    read.then((value) => ({ value, ok: true })),
    { value: fallback, ok: false }
  );
}

type OverviewReads = {
  enrollment: Promise<Panel<EnrollmentWire>>;
  fleet: Promise<Panel<ShardSnapshot>>;
  health: Promise<Panel<ServiceHealth[]>>;
  audit: Promise<Panel<AuditEntry[]>>;
  bot: Promise<Panel<boolean>>;
};

function liveReads(actorId: string, days: EnrollmentWindow, withAudit: boolean): OverviewReads {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  return {
    enrollment: panel(userEnrollment(actorId, days), emptyEnrollment()),
    fleet: panel(shardSnapshot(), emptyShardSnapshot()),
    health: panel(serviceHealth(), []),
    audit: panel(withAudit ? auditList(AUDIT_PEEK) : Promise.resolve([]), []),
    bot: panel(
      botId ? tokenStatus({ actorId, userId: botId }).then((t) => t.present) : Promise.resolve(false),
      false
    )
  };
}

function demoReads(days: EnrollmentWindow, withAudit: boolean): OverviewReads {
  const fixtures = import('$lib/server/demo-data');
  const from = <T>(pick: (f: Awaited<typeof fixtures>) => T): Promise<Panel<T>> =>
    fixtures.then((f) => ({ value: pick(f), ok: true }));
  return {
    enrollment: from((f) => f.demoEnrollment(days)),
    fleet: from((f) => f.sampleSnapshot),
    health: from((f) => f.sampleHealth),
    audit: from((f) => (withAudit ? f.sampleAudit : [])),
    bot: from(() => true)
  };
}

// Every read is returned as an unawaited promise so SvelteKit streams it: the
// page shell renders immediately and each panel hydrates when its own round
// trip lands, instead of blocking SSR on NATS.
export const load: PageServerLoad = async ({ url, parent }) => {
  const { id, role } = await parent();
  const withAudit = allows(role, 'audit.read');
  const days = parseEnrollmentWindow(url.searchParams.get('days'));

  return {
    days,
    ...(DEMO ? demoReads(days, withAudit) : liveReads(id, days, withAudit)),
    // Client-side visibility mirrors of the server ladder. The bot consent flow
    // mints a live Twitch credential, so its card is owner-only; a moderator
    // simply never sees the panel rather than being bounced by the route.
    canReadAudit: withAudit,
    canLinkBot: allows(role, 'bot.token'),
    canNotify: allows(role, 'notifications.send')
  };
};
