// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import {
  shardSnapshot,
  trialList,
  userEnrollment,
  tokenStatus,
  auditList,
  serviceHealth,
  type EnrollmentWire,
  type AuditEntry,
  type ServiceHealth,
  type TrialSnapshot
} from '$lib/server/services';
import { dev } from '$app/environment';
import { allows } from '$lib/server/access';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { emptyEnrollment, emptyShardSnapshot } from '$lib/server/fallback';
import { env } from '$env/dynamic/private';
import type { ShardSnapshot } from '@bagel/kit';
import { parseEnrollmentWindow, type EnrollmentWindow } from '$lib/enrollment-window';
import { giveawayAlerts, type GiveawayAlertWire } from '$lib/server/giveaways';

const AUDIT_PEEK = 6;
const DEMO = dev && process.env.DEMO === '1';

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
  trials: Promise<Panel<TrialSnapshot>>;
  health: Promise<Panel<ServiceHealth[]>>;
  audit: Promise<Panel<AuditEntry[]>>;
  bot: Promise<Panel<boolean>>;
  giveawayAlerts: Promise<Panel<GiveawayAlertWire[]>>;
};

const EMPTY_TRIALS: TrialSnapshot = { version: 1, trials: [] };

type OverviewPermissions = {
  withAudit: boolean;
  withGiveaways: boolean;
  withTrials: boolean;
};

function liveReads(
  actorId: string,
  days: EnrollmentWindow,
  { withAudit, withGiveaways, withTrials }: OverviewPermissions
): OverviewReads {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  return {
    enrollment: panel(userEnrollment(actorId, days), emptyEnrollment()),
    fleet: panel(shardSnapshot(), emptyShardSnapshot()),
    trials: panel(withTrials ? trialList() : Promise.resolve(EMPTY_TRIALS), EMPTY_TRIALS),
    health: panel(serviceHealth(), []),
    audit: panel(withAudit ? auditList(AUDIT_PEEK) : Promise.resolve([]), []),
    bot: panel(
      botId ? tokenStatus({ actorId, userId: botId }).then((t) => t.present) : Promise.resolve(false),
      false
    ),
    giveawayAlerts: panel(withGiveaways ? giveawayAlerts({ actorId }) : Promise.resolve([]), [])
  };
}

function demoReads(days: EnrollmentWindow, withAudit: boolean): OverviewReads {
  const fixtures = import('$lib/server/demo-data');
  const from = <T>(pick: (f: Awaited<typeof fixtures>) => T): Promise<Panel<T>> =>
    fixtures.then((f) => ({ value: pick(f), ok: true }));
  return {
    enrollment: from((f) => f.demoEnrollment(days)),
    fleet: from((f) => f.sampleSnapshot),
    trials: from(() => EMPTY_TRIALS),
    health: from((f) => f.sampleHealth),
    audit: from((f) => (withAudit ? f.sampleAudit : [])),
    bot: from(() => true),
    giveawayAlerts: from(() => [])
  };
}

export const load: PageServerLoad = async ({ url, parent }) => {
  const { id, role } = await parent();
  const withAudit = allows(role, 'audit.read');
  const withGiveaways = allows(role, 'giveaways.manage');
  const withTrials = allows(role, 'trials.manage');
  const days = parseEnrollmentWindow(url.searchParams.get('days'));

  return {
    days,
    ...(DEMO
      ? demoReads(days, withAudit)
      : liveReads(id, days, { withAudit, withGiveaways, withTrials })),
    canReadAudit: withAudit,
    canLinkBot: allows(role, 'bot.token'),
    canNotify: allows(role, 'notifications.send'),
    canViewTrials: withTrials
  };
};
