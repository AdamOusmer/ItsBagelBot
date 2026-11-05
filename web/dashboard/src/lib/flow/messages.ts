// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import type { Issue } from '@bagel/kit/automations';
import type { FlowWarning, UpgradeReport } from '#lib/server/flow-store';
import type { DeleteReply, SaveReply, ToggleReply } from './replies';

export type Translate = (key: MessageKey, params?: Record<string, string | number>) => string;

const lastSegment = (path: string) => path.split('.').pop() ?? '';

function issueTexts(t: Translate, issue: Issue): Record<string, string> {
  return {
    no_trigger: t('flows.issue.noTrigger'),
    multiple_triggers: t('flows.issue.multipleTriggers'),
    cycle: t('flows.issue.cycle'),
    unconnected_node: t('flows.issue.unconnectedNode'),
    unstructured_merge: t('flows.issue.unstructuredMerge'),
    unsupported_node: t('flows.issue.unsupportedNode'),
    unsupported_trigger: t('flows.issue.unsupportedTrigger'),
    unsupported_condition: t('flows.issue.unsupportedCondition'),
    unsupported_action: t('flows.issue.unsupportedAction'),
    wait_too_long: t('flows.issue.waitTooLong'),
    unmappable_token: t('flows.issue.unmappableToken', { token: `{${lastSegment(issue.path)}}` }),
    user_not_resolved: t('flows.issue.userNotResolved'),
    uses_token: t('flows.issue.usesToken', { count: '{count}', uses: '{uses}' }),
    command_exists: t('flows.issue.commandExists'),
    alert_exists: t('flows.issue.alertExists'),
    duplicate: t('flows.issue.duplicate'),
    too_many: t('flows.issue.tooMany'),
    required: t('flows.issue.required'),
    too_long: t('flows.issue.tooLong'),
    too_short: t('flows.issue.tooShort'),
    out_of_range: t('flows.issue.outOfRange'),
    invalid_enum: t('flows.issue.invalidEnum'),
    invalid_format: t('flows.issue.invalidFormat'),
    invalid_regex: t('flows.issue.invalidRegex'),
    delay_budget: t('flows.issue.delayBudget'),
    setup_required: t('flows.setup.refused')
  };
}

export function issueMessage(t: Translate, issue: Issue): string {
  if (issue.path === 'name') return t('flows.issue.name');
  const texts = issueTexts(t, issue);
  return Object.hasOwn(texts, issue.code) ? texts[issue.code]! : t('flows.issue.generic', { code: issue.code });
}

const nodeIdIn = (path: string) => /^nodes\.([^.]+)/.exec(path)?.[1];

export function stepOf(issues: readonly Issue[], nodes: readonly { id: string; type: string }[]): string | null {
  const known = new Set(nodes.map((node) => node.id));
  const trigger = nodes.find((node) => node.type.startsWith('twitch.'))?.id;
  for (const issue of issues) {
    const id = nodeIdIn(issue.path) ?? (issue.path.startsWith('trigger') ? trigger : undefined);
    if (id && known.has(id)) return id;
  }
  return null;
}

export function saveMessage(t: Translate, reply: Extract<SaveReply, { ok: false }>): string {
  switch (reply.reason) {
    case 'invalid':
    case 'conflict':
      return reply.issues?.[0] ? issueMessage(t, reply.issues[0]) : t('flows.saveFailed');
    case 'not_found':
      return t('flows.saveNotFound');
    case 'denied':
      return reply.error ?? t('flows.saveDenied');
    default:
      return t('flows.saveFailed');
  }
}

export const isStale = (reply: Extract<SaveReply, { ok: false }>) => reply.reason === 'conflict' && (reply.issues ?? []).length === 0;

function warningTexts(t: Translate): Record<FlowWarning, string> {
  return {
    shadowed_by_command: t('flows.warnShadowed'),
    command_delete_failed: t('flows.warnCommandDelete'),
    automation_delete_failed: t('flows.warnAutomationDelete'),
    alert_off_failed: t('flows.warnAlertOff'),
    alerts_module_turned_on: t('flows.warnAlertsOn')
  };
}

export function warningMessages(t: Translate, warnings: readonly FlowWarning[]): string[] {
  const texts = warningTexts(t);
  return warnings.map((warning) => texts[warning]);
}

export function toggleMessage(t: Translate, reply: Extract<ToggleReply, { ok: false }>): string {
  if (reply.reason === 'conflict') return t('flows.toggleStale');
  if (reply.reason === 'not_found') return t('flows.toggleNotFound');
  if (reply.reason === 'needs_setup') return t('flows.setup.refused');
  return reply.reason === 'denied' && reply.error ? reply.error : t('flows.toggleFailed');
}

export function deleteMessage(t: Translate, reply: Extract<DeleteReply, { ok: false }>): string {
  if (reply.reason === 'not_found') return t('flows.deleteGone');
  if (reply.reason === 'not_deletable') return t('flows.deleteBuiltin');
  return reply.reason === 'denied' && reply.error ? reply.error : t('flows.deleteFailed');
}

const when = (condition: unknown, text: () => string) => (condition ? [text()] : []);

function alertNotes(t: Translate, report: Extract<UpgradeReport, { from: 'alert' }>): string[] {
  return [
    ...when(report.repeatGuardWindowHours > 0, () => t('flows.upgradeRepeat', { hours: report.repeatGuardWindowHours })),
    ...when(report.resubsNoLongerCovered, () => t('flows.upgradeResubs'))
  ];
}

function commandNotes(t: Translate, report: Exclude<UpgradeReport, { from: 'alert' }>): string[] {
  return [
    ...when(report.uses > 0 || report.usesTokenInReply, () => t('flows.upgradeUses', { count: '{count}', uses: '{uses}' })),
    ...when(report.streamOnlineOnly, () => t('flows.upgradeLive')),
    ...when(report.allowedUserId, () => t('flows.upgradeUser')),
    ...when(report.bumpCounter, () => t('flows.upgradeCounter', { counter: report.bumpCounter }))
  ];
}

export function upgradeNotes(t: Translate, report: UpgradeReport): string[] {
  return report.from === 'alert' ? alertNotes(t, report) : commandNotes(t, report);
}
