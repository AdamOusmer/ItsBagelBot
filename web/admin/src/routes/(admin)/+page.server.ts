// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import {
  shardSnapshot,
  trialList,
  userEnrollment,
  userStats,
  tokenStatus,
  auditList,
  serviceHealth,
  feedBoard,
  type EnrollmentWire,
  type FeedBoard,
  type AuditEntry,
  type ServiceHealth,
  type TrialSnapshot
} from '#lib/server/services';
import { allows } from '#lib/access';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { EMPTY_USER_STATS, emptyEnrollment, emptyFeedBoard, emptyShardSnapshot } from '#lib/server/fallback';
import { env } from '@bagel/kit/server/env';
import type { ShardSnapshot, UserStats } from '@bagel/kit';
import { parseEnrollmentWindow, type EnrollmentWindow } from '#lib/enrollment-window';
import { giveawayAlerts, type GiveawayAlertWire } from '#lib/server/giveaways';

const AUDIT_PEEK = 6;

export type Panel<T> = { value: T; ok: boolean };

function panel<T>(read: Promise<T>, fallback: T): Promise<Panel<T>> {
  return bestEffort(
    read.then((value) => ({ value, ok: true })),
    { value: fallback, ok: false }
  );
}

type OverviewReads = {
  accountStats: Promise<Panel<UserStats>>;
  enrollment: Promise<Panel<EnrollmentWire>>;
  fleet: Promise<Panel<ShardSnapshot>>;
  trials: Promise<Panel<TrialSnapshot>>;
  health: Promise<Panel<ServiceHealth[]>>;
  feed: Promise<Panel<FeedBoard>>;
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
  const botId = env.TWITCH_BOT_USER_ID ?? '';
  return {
    accountStats: panel(userStats(actorId), EMPTY_USER_STATS),
    enrollment: panel(userEnrollment(actorId, days), emptyEnrollment(days)),
    fleet: panel(shardSnapshot(), emptyShardSnapshot()),
    trials: panel(withTrials ? trialList() : Promise.resolve(EMPTY_TRIALS), EMPTY_TRIALS),
    health: panel(serviceHealth(), []),
    feed: panel(feedBoard(), emptyFeedBoard()),
    audit: panel(withAudit ? auditList(AUDIT_PEEK) : Promise.resolve([]), []),
    bot: panel(
      botId ? tokenStatus({ actorId, userId: botId }).then((t) => t.present) : Promise.resolve(false),
      false
    ),
    giveawayAlerts: panel(withGiveaways ? giveawayAlerts({ actorId }) : Promise.resolve([]), [])
  };
}

export const load = (async ({ url, parent }) => {
  const { id, role } = await parent();
  const withAudit = allows(role, 'audit.read');
  const withGiveaways = allows(role, 'giveaways.manage');
  const withTrials = allows(role, 'trials.manage');
  const days = parseEnrollmentWindow(url.searchParams.get('days'));

  return {
    days,
    ...liveReads(id, days, { withAudit, withGiveaways, withTrials }),
    botLink: `${url.origin}/auth/bot/login`,
    canReadAudit: withAudit,
    canLinkBot: allows(role, 'bot.token'),
    canNotify: allows(role, 'notifications.send'),
    canViewGiveaways: withGiveaways,
    canViewTrials: withTrials
  };
}) satisfies PageServerLoad;
