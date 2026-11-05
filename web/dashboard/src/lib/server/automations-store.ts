// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { defineRead, defineWrite } from '@bagel/kit/server/service';
import {
  RUNS_KEPT,
  type Automation,
  type AutomationSummary,
  type Definition,
  type DeleteReply,
  type GetReply,
  type Issue,
  type ListReply,
  type Refusal,
  type RunReply,
  type RunsReply,
  type RunRecord,
  type SummariesReply,
  type UpsertReply,
  type ValidateReply
} from '@bagel/kit/automations';
import { SUB, fabric, invalidate } from './services';

const subject = (verb: string) => `${SUB.modules}.automation.${verb}`;
const listKey = (userId: string) => `automations:${userId}`;

export interface AutomationInput {
  id?: string;
  name: string;
  enabled: boolean;
  definition: Definition;
  preview?: string;
  expectedRevision?: number;
}

export type UpsertResult =
  | { ok: true; automation: Automation }
  | { ok: false; error?: string; code?: string; issues: Issue[]; conflict: boolean };

export type RunNowResult = { ok: true; runId?: string } | { ok: false; error?: string; code?: string };

function refused(r: Refusal): string | undefined {
  return r.error || r.code || undefined;
}

function requireOk<T extends Refusal>(r: T): T {
  const reason = refused(r);
  if (reason) throw new Error(reason);
  return r;
}

export const listAutomations = defineRead({
  subject: subject('list'),
  request: (userId: string) => ({ user_id: userId }),
  map: (r: ListReply): Automation[] => requireOk(r).automations ?? [],
  cache: { fabric, key: listKey, policy: POLICY.entity }
});

export const listAutomationSummaries = defineRead({
  subject: subject('summaries'),
  request: (userId: string) => ({ user_id: userId }),
  map: (r: SummariesReply): AutomationSummary[] => requireOk(r).automations ?? [],
  cache: { fabric, key: (userId: string) => `${listKey(userId)}:summaries`, policy: POLICY.entity }
});

export const getAutomation = defineRead({
  subject: subject('get'),
  request: (userId: string, id: string) => ({ user_id: userId, id }),
  map: (r: GetReply): Automation | null => (r.code === 'not_found' ? null : (requireOk(r).automation ?? null))
});

export const listRuns = defineRead({
  subject: `${SUB.automations}.runs`,
  request: (userId: string, automationId?: string, limit: number = RUNS_KEPT) => ({
    broadcaster_id: userId,
    automation_id: automationId || undefined,
    limit
  }),
  map: (r: RunsReply): RunRecord[] => requireOk(r).runs ?? []
});

function upsertResult(r: UpsertReply): UpsertResult {
  const issues = r.issues ?? [];
  const failed = !!refused(r) || !!r.conflict || issues.length > 0 || !r.automation;
  if (!failed && r.automation) return { ok: true, automation: r.automation };
  const conflict = r.conflict === true || (r.code === 'conflict' && issues.length === 0);
  return { ok: false, error: r.error, code: r.code, issues, conflict };
}

export const upsertAutomation = defineWrite({
  subject: subject('upsert'),
  request: (userId: string, input: AutomationInput) => ({
    user_id: userId,
    id: input.id || undefined,
    name: input.name,
    enabled: input.enabled,
    definition: input.definition,
    preview: input.preview,
    expected_revision: input.expectedRevision
  }),
  map: upsertResult,
  after: (_result: UpsertResult, userId: string) => invalidate(listKey(userId))
});

export const setAutomationEnabled = defineWrite({
  subject: subject('set_enabled'),
  request: (userId: string, id: string, enabled: boolean, expectedRevision?: number) => ({
    user_id: userId,
    id,
    enabled,
    expected_revision: expectedRevision
  }),
  map: upsertResult,
  after: (_result: UpsertResult, userId: string) => invalidate(listKey(userId))
});

export interface AlertUpgradeInput {
  name: string;
  enabled: boolean;
  definition: Definition;
  preview?: string;
  enableKey: string;
  expectedModuleRev?: number;
}

export const upgradeAlertToAutomation = defineWrite({
  subject: subject('upgrade_alert'),
  request: (userId: string, input: AlertUpgradeInput) => ({
    user_id: userId,
    name: input.name,
    enabled: input.enabled,
    definition: input.definition,
    preview: input.preview,
    enable_key: input.enableKey,
    expected_module_rev: input.expectedModuleRev
  }),
  map: upsertResult,
  after: (_result: UpsertResult, userId: string) => invalidate(listKey(userId), `modules:${userId}`)
});

export interface AlertDowngradeInput {
  id: string;
  enableKey: string;
  message: string;
  enabled: boolean;
  expectedRevision?: number;
  expectedModuleRev?: number;
}

export type AlertDowngradeResult = { ok: true } | { ok: false; conflict: boolean; code?: string; error?: string };

export const downgradeAutomationToAlert = defineWrite({
  subject: subject('downgrade_alert'),
  request: (userId: string, input: AlertDowngradeInput) => ({
    user_id: userId,
    id: input.id,
    enable_key: input.enableKey,
    message: input.message,
    enabled: input.enabled,
    expected_revision: input.expectedRevision,
    expected_module_rev: input.expectedModuleRev
  }),
  map: (r: Refusal & { conflict?: boolean }): AlertDowngradeResult =>
    refused(r) || r.conflict ? { ok: false, conflict: r.conflict === true, code: r.code, error: r.error } : { ok: true },
  after: (_result: AlertDowngradeResult, userId: string) => invalidate(listKey(userId), `modules:${userId}`)
});

export const deleteAutomation = defineWrite({
  subject: subject('delete'),
  request: (userId: string, id: string) => ({ user_id: userId, id }),
  map: (r: DeleteReply): boolean => !refused(r) && r.deleted === true,
  after: (_result: boolean, userId: string) => invalidate(listKey(userId))
});

export const validateAutomation = defineWrite({
  subject: subject('validate'),
  request: (userId: string, name: string, definition: Definition) => ({ user_id: userId, name, definition }),
  map: (r: ValidateReply): Issue[] => requireOk(r).issues ?? []
});

export const runNow = defineWrite({
  subject: `${SUB.automations}.run`,
  request: (userId: string, name: string) => ({
    broadcaster_id: userId,
    name,
    source: 'dashboard',
    request_id: randomUUID()
  }),
  map: (r: RunReply): RunNowResult =>
    r.accepted ? { ok: true, runId: r.run_id } : { ok: false, error: r.error, code: r.code }
});
