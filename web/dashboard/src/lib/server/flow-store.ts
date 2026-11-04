// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';
import {
  ALERT_KEYS,
  alertDefinition,
  classifyFlow,
  commandDefinition,
  plainAlertLine,
  validateDefinition,
  type AlertKey,
  type Automation,
  type Definition,
  type FlowLayer,
  type Issue,
  type SimpleCommand
} from '@bagel/kit/automations';
import { moduleDef, MOD } from '@bagel/kit/catalog';
import type { CommandView, Perm } from '@bagel/kit';
import { rpc } from '@bagel/kit/server/nats';
import type { FlowRef } from '#lib/flow/ref';
import {
  deleteAutomation,
  downgradeAutomationToAlert,
  getAutomation,
  listAutomations,
  listAutomationSummaries,
  setAutomationEnabled,
  upgradeAlertToAutomation,
  upsertAutomation,
  type AlertDowngradeResult,
  type UpsertResult
} from './automations-store';
import {
  deleteCommand,
  listModules,
  listModulesForEditing,
  patchModule,
  upsertCommand,
  type ModuleView
} from './commands-store';
import { effectiveId } from './board';
import { moduleWriteDenial } from './module-gate';
import { SUB } from './services';

type UserId = string;
type FlowId = string;
type CommandName = string;
type Revision = string;
type Enabled = boolean;
type AlertLine = string;

export type { FlowRef };

export interface StoredFlow {
  ref: FlowRef;
  name: string;
  enabled: boolean;
  revision: string;
  definition: Definition;
}

export interface SaveFlowInput {
  ref: FlowRef | null;
  name: string;
  enabled: boolean;
  definition: Definition;
  preview?: string;
  expectedRevision?: string;
  confirmUpgrade?: boolean;
}

export interface FlowSummary {
  ref: FlowRef;
  name: string;
  enabled: boolean;
  revision: string;
  preview: string | null;
}

export type CommandUpgradeReport = {
  from: 'command';
  uses: number;
  usesTokenInReply: boolean;
  streamOnlineOnly: boolean;
  allowedUserId: string;
  bumpCounter: string;
};

export type AlertUpgradeReport = {
  from: 'alert';
  alert: AlertKey;
  repeatGuardWindowHours: number;
  resubsNoLongerCovered: boolean;
};

export type UpgradeReport = CommandUpgradeReport | AlertUpgradeReport;

export type FlowWarning =
  | 'automations_module_off'
  | 'shadowed_by_command'
  | 'command_delete_failed'
  | 'automation_delete_failed'
  | 'alert_off_failed'
  | 'alerts_module_turned_on';

export type FlowDenied = { ok: false; reason: 'denied'; error: string };

export type SetFlowEnabledResult =
  | { ok: true; enabled: boolean; revision: string; warnings: FlowWarning[] }
  | { ok: false; reason: 'conflict' | 'not_found' }
  | { ok: false; reason: 'failed'; error: string }
  | FlowDenied;

export type DeleteFlowResult =
  | { ok: true }
  | { ok: false; reason: 'not_found' | 'not_deletable' }
  | { ok: false; reason: 'failed'; error: string }
  | FlowDenied;

export type SaveFlowResult =
  | { ok: true; flow: StoredFlow; upgrade?: UpgradeReport; warnings: FlowWarning[] }
  | { ok: false; reason: 'conflict'; issues: Issue[] }
  | { ok: false; reason: 'invalid'; issues: Issue[] }
  | { ok: false; reason: 'not_found' }
  | { ok: false; reason: 'needs_upgrade'; upgrade: UpgradeReport }
  | { ok: false; reason: 'failed'; error: string }
  | FlowDenied;

const ALERT_REPEAT_GUARD_HOURS: Partial<Record<AlertKey, number>> = { follow: 72 };
const SUB_REPEAT_GUARD_HOURS = 0.25;

const replyFor = (alert: AlertKey) => moduleDef(MOD.alerts)?.replies.find((reply) => reply.key === alert);

const enableKey = (alert: AlertKey) => `${alert}Enabled`;
const messageKey = (alert: AlertKey) => `${alert}Message`;

function token(parts: unknown): string {
  return createHash('sha256').update(JSON.stringify(parts)).digest('base64url').slice(0, 16);
}

function commandToken(row: CommandView): string {
  return token([
    row.name,
    row.aliases ?? [],
    row.response,
    row.is_active,
    row.stream_online_only === true,
    row.perm ?? 'everyone',
    row.cooldown ?? 0,
    row.user_cooldown ?? 0,
    row.allowed_user_id ?? '',
    row.bump_counter ?? ''
  ]);
}

function simpleCommand(row: CommandView): SimpleCommand {
  return {
    name: row.name,
    aliases: row.aliases ?? [],
    response: row.response,
    perm: row.perm ?? 'everyone',
    cooldown: row.cooldown ?? 0,
    userCooldown: row.user_cooldown ?? 0
  };
}

function commandFlow(row: CommandView): StoredFlow {
  const definition = commandDefinition(simpleCommand(row));
  return { ref: { layer: 'command', name: row.name }, name: `!${row.name}`, enabled: row.is_active, revision: commandToken(row), definition };
}

type AlertState = { on: boolean; customized: boolean; message: string; configured: unknown[] };

function alertState(alert: AlertKey, row: ModuleView | undefined): AlertState {
  const configs = (row?.configs ?? {}) as Record<string, unknown>;
  const flag = configs[enableKey(alert)];
  const stored = configs[messageKey(alert)];
  const customized = typeof stored === 'string' && stored !== '';
  const message = plainAlertLine(customized ? stored : (replyFor(alert)?.defaultMessage ?? ''));
  return { on: row?.is_enabled === true && flag !== 'off', customized, message, configured: [row?.is_enabled === true, flag, stored] };
}

function alertFlow(alert: AlertKey, row: ModuleView | undefined): StoredFlow {
  const state = alertState(alert, row);
  return {
    ref: { layer: 'alert', alert },
    name: replyFor(alert)?.label ?? alert,
    enabled: state.on,
    revision: token(state.configured),
    definition: alertDefinition(alert, state.message)
  };
}

function automationFlow(row: Automation): StoredFlow {
  return { ref: { layer: 'automation', id: row.id }, name: row.name, enabled: row.enabled, revision: String(row.revision), definition: row.definition };
}

async function ownerCommands(userId: UserId): Promise<CommandView[]> {
  const reply = await rpc<{ commands?: CommandView[] }>(`${SUB.commands}.list`, { user_id: userId });
  return reply.commands ?? [];
}

async function alertsRow(userId: UserId): Promise<ModuleView | undefined> {
  return (await listModulesForEditing(userId)).find((row) => row.name === MOD.alerts);
}

const drawableCommand = (row: CommandView) => row.response !== '' && row.builtin !== true;

export async function listFlows(userId: UserId): Promise<StoredFlow[]> {
  const [automations, commands, alerts] = await Promise.all([listAutomations(userId), ownerCommands(userId), alertsRow(userId)]);
  return [
    ...automations.map(automationFlow),
    ...commands.filter(drawableCommand).map(commandFlow),
    ...ALERT_KEYS.map((alert) => alertFlow(alert, alerts))
  ];
}

export async function listFlowSummaries(userId: UserId): Promise<FlowSummary[]> {
  const automations = await listAutomationSummaries(userId);
  return automations.map(
    (row): FlowSummary => ({
      ref: { layer: 'automation', id: row.id },
      name: row.name,
      enabled: row.enabled,
      revision: String(row.revision),
      preview: row.preview ?? null
    })
  );
}

export async function loadFlow(userId: UserId, ref: FlowRef): Promise<StoredFlow | null> {
  if (ref.layer === 'automation') {
    const row = await getAutomation(userId, ref.id);
    return row ? automationFlow(row) : null;
  }
  if (ref.layer === 'alert') return alertFlow(ref.alert, await alertsRow(userId));
  const row = (await ownerCommands(userId)).find((c) => c.name === ref.name);
  return row && drawableCommand(row) ? commandFlow(row) : null;
}

const CONFLICT: SaveFlowResult = { ok: false, reason: 'conflict', issues: [] };
const COMMAND_EXISTS: SaveFlowResult = { ok: false, reason: 'conflict', issues: [{ path: 'trigger.params.name', code: 'command_exists' }] };
const ALERT_EXISTS: SaveFlowResult = { ok: false, reason: 'conflict', issues: [{ path: 'trigger', code: 'alert_exists' }] };

const USES_TOKEN = /\{(count|uses)\}/;

// Automations have no use counter, so these tokens would render a permanent 0.
function usesTokenRefusal(definition: Definition): SaveFlowResult | null {
  if (!USES_TOKEN.test(JSON.stringify(definition.actions ?? []))) return null;
  return { ok: false, reason: 'invalid', issues: [{ path: 'actions', code: 'uses_token' }] };
}

function failed(err: unknown): SaveFlowResult {
  return { ok: false, reason: 'failed', error: err instanceof Error ? err.message : String(err) };
}

const staleRevision = (expected: Revision | undefined, current: Revision) => expected !== undefined && expected !== current;

function commandInput(command: SimpleCommand, enabled: Enabled, current: CommandView | undefined) {
  return {
    name: command.name,
    aliases: command.aliases,
    response: command.response,
    isActive: enabled,
    streamOnlineOnly: current?.stream_online_only === true,
    perm: command.perm as Perm,
    cooldown: command.cooldown,
    userCooldown: command.userCooldown,
    allowedUserId: current?.allowed_user_id ?? '',
    bumpCounter: current?.bump_counter ?? ''
  };
}

function commandNameTaken(rows: CommandView[], command: SimpleCommand, previousName?: CommandName): boolean {
  const wanted = new Set([command.name, ...command.aliases].map((name) => name.toLowerCase()));
  return rows.some((row) => row.name !== previousName && [row.name, ...(row.aliases ?? [])].some((name) => wanted.has(name.toLowerCase())));
}

// Deletes rows that classify as the layer just written. A failed delete keeps the row's simple definition, so the next save of that flow retries it.
async function sweepMisplacedRows(userId: UserId, belongsHere: (layer: FlowLayer) => boolean): Promise<FlowWarning[]> {
  const rows = (await listAutomations(userId)).filter((row) => belongsHere(classifyFlow(row.definition)));
  const deleted = await Promise.all(rows.map((row) => deleteAutomation(userId, row.id).catch(() => false)));
  return deleted.every(Boolean) ? [] : ['automation_delete_failed'];
}

const sameCommand = (name: CommandName) => (layer: FlowLayer) => layer.layer === 'command' && layer.command.name.toLowerCase() === name.toLowerCase();
const sameAlert = (alert: AlertKey) => (layer: FlowLayer) => layer.layer === 'alert' && layer.alert === alert;

async function writeCommand(userId: UserId, command: SimpleCommand, input: SaveFlowInput, previousName?: CommandName): Promise<SaveFlowResult> {
  const rows = await ownerCommands(userId);
  const current = previousName ? rows.find((row) => row.name === previousName) : undefined;
  if (previousName && !current) return { ok: false, reason: 'not_found' };
  if (current && staleRevision(input.expectedRevision, commandToken(current))) return CONFLICT;
  if (commandNameTaken(rows, command, previousName)) return COMMAND_EXISTS;
  const saved = commandInput(command, input.enabled, current);
  await upsertCommand(userId, saved, previousName);
  const view: CommandView = { name: saved.name, aliases: saved.aliases, response: saved.response, is_active: saved.isActive, stream_online_only: saved.streamOnlineOnly, perm: saved.perm, cooldown: saved.cooldown, user_cooldown: saved.userCooldown, allowed_user_id: saved.allowedUserId, bump_counter: saved.bumpCounter };
  return { ok: true, flow: commandFlow(view), warnings: await sweepMisplacedRows(userId, sameCommand(command.name)) };
}

function alertLineTaken(state: AlertState, message: AlertLine, enabled: Enabled): boolean {
  const changesLine = state.message !== message;
  return (state.customized && changesLine) || (state.on && (changesLine || !enabled));
}

const rowHoldsAlert = (rows: Automation[], alert: AlertKey) => rows.some((row) => sameAlert(alert)(classifyFlow(row.definition)));

type WantedAlert = { alert: AlertKey; message: AlertLine; enabled: Enabled };

async function alertRefusal(userId: UserId, wanted: WantedAlert, row: ModuleView | undefined, exceptId?: FlowId): Promise<SaveFlowResult | null> {
  const rows = (await listAutomations(userId)).filter((other) => other.id !== exceptId);
  return alertLineTaken(alertState(wanted.alert, row), wanted.message, wanted.enabled) || rowHoldsAlert(rows, wanted.alert) ? ALERT_EXISTS : null;
}

export async function takenAlerts(userId: UserId): Promise<AlertKey[]> {
  const [row, rows] = await Promise.all([alertsRow(userId), listAutomations(userId)]);
  return ALERT_KEYS.filter((alert) => {
    const state = alertState(alert, row);
    return state.on || state.customized || rowHoldsAlert(rows, alert);
  });
}

const keepsModuleOn = (enabled: Enabled, row: ModuleView | undefined) => enabled || row?.is_enabled === true;

const alertSwitch = (alert: AlertKey, enabled: Enabled) => ({ [enableKey(alert)]: enabled ? 'on' : 'off' });

async function writeAlert(userId: UserId, alert: AlertKey, message: AlertLine, input: SaveFlowInput): Promise<SaveFlowResult> {
  const row = await alertsRow(userId);
  const state = alertState(alert, row);
  if (staleRevision(input.expectedRevision, token(state.configured))) return CONFLICT;
  const refusal = input.ref === null ? await alertRefusal(userId, { alert, message, enabled: input.enabled }, row) : null;
  if (refusal) return refusal;
  const patched = await patchModule({
    userId,
    name: MOD.alerts,
    isEnabled: keepsModuleOn(input.enabled, row),
    partial: { ...alertSwitch(alert, input.enabled), [messageKey(alert)]: message },
    expectedRev: row?.revision ?? 0
  });
  if (patched.conflict) return CONFLICT;
  const flow = await loadFlow(userId, { layer: 'alert', alert });
  const swept = await sweepMisplacedRows(userId, sameAlert(alert));
  return { ok: true, flow: flow!, warnings: [...alertWarnings(input.enabled, row), ...swept] };
}

const alertWarnings = (enabled: Enabled, row: ModuleView | undefined): FlowWarning[] =>
  enabled && row?.is_enabled !== true ? ['alerts_module_turned_on'] : [];

function upsertFailure(result: Extract<UpsertResult, { ok: false }>): SaveFlowResult {
  if (result.conflict) return { ok: false, reason: 'conflict', issues: result.issues };
  if (result.issues.length > 0) return { ok: false, reason: 'invalid', issues: result.issues };
  return { ok: false, reason: 'failed', error: result.error ?? result.code ?? 'save failed' };
}

function triggerNames(definition: Definition): string[] {
  if (definition.trigger.type !== 'command') return [];
  const params = definition.trigger.params ?? {};
  return [params.name, ...((params.aliases ?? []) as string[])].map((n) => String(n).replace(/^!/, '').toLowerCase());
}

async function automationWarnings(userId: UserId, definition: Definition, enabled: Enabled, dropping?: CommandName): Promise<FlowWarning[]> {
  const [modules, commands] = await Promise.all([listModules(userId), ownerCommands(userId)]);
  const warnings: FlowWarning[] = [];
  if (enabled && modules.find((row) => row.name === MOD.automations)?.is_enabled !== true) warnings.push('automations_module_off');
  const names = triggerNames(definition);
  if (commands.some((row) => row.name !== dropping && names.includes(row.name.toLowerCase()))) warnings.push('shadowed_by_command');
  return warnings;
}

async function writeAutomation(userId: UserId, input: SaveFlowInput, id?: FlowId, dropping?: CommandName): Promise<SaveFlowResult> {
  const refusal = usesTokenRefusal(input.definition);
  if (refusal) return refusal;
  const expected = id && input.expectedRevision !== undefined ? Number(input.expectedRevision) : undefined;
  const result = await upsertAutomation(userId, { id, name: input.name, enabled: input.enabled, definition: input.definition, preview: input.preview, expectedRevision: expected });
  if (!result.ok) return upsertFailure(result);
  return { ok: true, flow: automationFlow(result.automation), warnings: await automationWarnings(userId, input.definition, input.enabled, dropping) };
}

const asNewFlow = (input: SaveFlowInput): SaveFlowInput => ({ ...input, ref: null, expectedRevision: undefined });

// The row takes the simple definition first, so a failed delete leaves something the next save's sweep recognises. A row whose command answers first never fires.
async function downgradeToCommand(userId: UserId, id: FlowId, command: SimpleCommand, input: SaveFlowInput): Promise<SaveFlowResult> {
  const row = await getAutomation(userId, id);
  if (!row) return { ok: false, reason: 'not_found' };
  if (staleRevision(input.expectedRevision, String(row.revision))) return CONFLICT;
  if (commandNameTaken(await ownerCommands(userId), command)) return COMMAND_EXISTS;
  const parked = await upsertAutomation(userId, { id, name: row.name, enabled: row.enabled, definition: input.definition, expectedRevision: row.revision });
  if (!parked.ok) return upsertFailure(parked);
  const written = await writeCommand(userId, command, asNewFlow(input)).catch(failed);
  if (written.ok) return written;
  await upsertAutomation(userId, { id, name: row.name, enabled: row.enabled, definition: row.definition, expectedRevision: parked.automation.revision }).catch(() => null);
  return written;
}

async function downgradeToAlert(userId: UserId, id: FlowId, target: Extract<FlowLayer, { layer: 'alert' }>, input: SaveFlowInput): Promise<SaveFlowResult> {
  const module = await alertsRow(userId);
  const refusal = await alertRefusal(userId, { alert: target.alert, message: target.message, enabled: input.enabled }, module, id);
  if (refusal) return refusal;
  const result = await downgradeAutomationToAlert(userId, {
    id,
    enableKey: enableKey(target.alert),
    message: target.message,
    enabled: input.enabled,
    expectedRevision: input.expectedRevision === undefined ? undefined : Number(input.expectedRevision),
    expectedModuleRev: module?.revision ?? 0
  });
  if (!result.ok) return downgradeFailure(result);
  const flow = await loadFlow(userId, { layer: 'alert', alert: target.alert });
  return { ok: true, flow: flow!, warnings: alertWarnings(input.enabled, module) };
}

function downgradeFailure(result: Extract<AlertDowngradeResult, { ok: false }>): SaveFlowResult {
  if (result.conflict) return CONFLICT;
  if (result.code === 'not_found') return { ok: false, reason: 'not_found' };
  return { ok: false, reason: 'failed', error: result.error ?? result.code ?? 'save failed' };
}

function saveAutomationRow(userId: UserId, id: FlowId, target: FlowLayer, input: SaveFlowInput): Promise<SaveFlowResult> {
  if (target.layer === 'command') return downgradeToCommand(userId, id, target.command, input);
  if (target.layer === 'alert') return downgradeToAlert(userId, id, target, input);
  return writeAutomation(userId, input, id);
}

function commandReport(row: CommandView): CommandUpgradeReport {
  return {
    from: 'command',
    uses: Number(row.uses ?? 0),
    usesTokenInReply: USES_TOKEN.test(row.response),
    streamOnlineOnly: row.stream_online_only === true,
    allowedUserId: row.allowed_user_id ?? '',
    bumpCounter: row.bump_counter ?? ''
  };
}

function alertReport(alert: AlertKey): AlertUpgradeReport {
  return {
    from: 'alert',
    alert,
    repeatGuardWindowHours: alert === 'sub' ? SUB_REPEAT_GUARD_HOURS : (ALERT_REPEAT_GUARD_HOURS[alert] ?? 0),
    resubsNoLongerCovered: alert === 'sub'
  };
}

// runCustom answers before automations, so until the delete lands the old command keeps replying and the automation stays quiet.
async function moveCommand(userId: UserId, ref: { name: CommandName }, target: FlowLayer, input: SaveFlowInput): Promise<SaveFlowResult> {
  const row = (await ownerCommands(userId)).find((c) => c.name === ref.name);
  if (!row) return { ok: false, reason: 'not_found' };
  if (staleRevision(input.expectedRevision, commandToken(row))) return CONFLICT;
  const upgrade = commandReport(row);
  if (!input.confirmUpgrade) return { ok: false, reason: 'needs_upgrade', upgrade };
  const written = await saveNew(userId, target, asNewFlow(input), row.name);
  if (!written.ok) return written;
  const deleted = await deleteCommand(userId, row.name).then(() => true, () => false);
  return { ...written, upgrade, warnings: deleted ? written.warnings : [...written.warnings, 'command_delete_failed'] };
}

async function moveAlertToSimple(userId: UserId, target: FlowLayer, input: SaveFlowInput, upgrade: AlertUpgradeReport): Promise<SaveFlowResult> {
  const written = await saveNew(userId, target, asNewFlow(input));
  if (!written.ok) return written;
  const off = await toggleAlert(userId, upgrade.alert, false);
  return { ...written, upgrade, warnings: off.ok ? written.warnings : [...written.warnings, 'alert_off_failed'] };
}

async function moveAlert(userId: UserId, alert: AlertKey, target: FlowLayer, input: SaveFlowInput): Promise<SaveFlowResult> {
  const row = await alertsRow(userId);
  if (staleRevision(input.expectedRevision, token(alertState(alert, row).configured))) return CONFLICT;
  const upgrade = alertReport(alert);
  if (!input.confirmUpgrade) return { ok: false, reason: 'needs_upgrade', upgrade };
  if (target.layer !== 'automation') return moveAlertToSimple(userId, target, input, upgrade);
  const refusal = usesTokenRefusal(input.definition);
  if (refusal) return refusal;
  const result = await upgradeAlertToAutomation(userId, {
    name: input.name,
    enabled: input.enabled,
    definition: input.definition,
    preview: input.preview,
    enableKey: enableKey(alert),
    expectedModuleRev: row?.revision ?? 0
  });
  if (!result.ok) return upsertFailure(result);
  return { ok: true, flow: automationFlow(result.automation), upgrade, warnings: await automationWarnings(userId, input.definition, input.enabled) };
}

function saveExistingCommand(userId: UserId, ref: { name: CommandName }, target: FlowLayer, input: SaveFlowInput) {
  if (target.layer === 'command') return writeCommand(userId, target.command, input, ref.name);
  return moveCommand(userId, ref, target, input);
}

function saveExistingAlert(userId: UserId, alert: AlertKey, target: FlowLayer, input: SaveFlowInput) {
  if (target.layer === 'alert' && target.alert === alert) return writeAlert(userId, alert, target.message, input);
  return moveAlert(userId, alert, target, input);
}

function saveNew(userId: UserId, target: FlowLayer, input: SaveFlowInput, droppingCommand?: CommandName): Promise<SaveFlowResult> {
  if (target.layer === 'command') return writeCommand(userId, target.command, input);
  if (target.layer === 'alert') return writeAlert(userId, target.alert, target.message, input);
  return writeAutomation(userId, input, undefined, droppingCommand);
}

function route(userId: UserId, target: FlowLayer, input: SaveFlowInput): Promise<SaveFlowResult> {
  const ref = input.ref;
  if (!ref) return saveNew(userId, target, input);
  if (ref.layer === 'automation') return saveAutomationRow(userId, ref.id, target, input);
  if (ref.layer === 'command') return saveExistingCommand(userId, ref, target, input);
  return saveExistingAlert(userId, ref.alert, target, input);
}

const TOGGLE_CONFLICT: SetFlowEnabledResult = { ok: false, reason: 'conflict' };
const TOGGLE_NOT_FOUND: SetFlowEnabledResult = { ok: false, reason: 'not_found' };

async function toggleAutomation(userId: UserId, id: FlowId, enabled: Enabled, expected?: Revision): Promise<SetFlowEnabledResult> {
  const result = await setAutomationEnabled(userId, id, enabled, expected === undefined ? undefined : Number(expected));
  if (!result.ok) {
    if (result.conflict) return TOGGLE_CONFLICT;
    if (result.code === 'not_found') return TOGGLE_NOT_FOUND;
    return { ok: false, reason: 'failed', error: result.error ?? result.code ?? 'toggle failed' };
  }
  const modules = await listModules(userId);
  const moduleOff = enabled && modules.find((row) => row.name === MOD.automations)?.is_enabled !== true;
  return { ok: true, enabled, revision: String(result.automation.revision), warnings: moduleOff ? ['automations_module_off'] : [] };
}

async function toggleCommand(userId: UserId, name: CommandName, enabled: Enabled, expected?: Revision): Promise<SetFlowEnabledResult> {
  const row = (await ownerCommands(userId)).find((c) => c.name === name);
  if (!row) return TOGGLE_NOT_FOUND;
  if (staleRevision(expected, commandToken(row))) return TOGGLE_CONFLICT;
  if (row.is_active === enabled) return { ok: true, enabled, revision: commandToken(row), warnings: [] };
  await upsertCommand(userId, commandInput(simpleCommand(row), enabled, row), name);
  return { ok: true, enabled, revision: commandToken({ ...row, is_active: enabled }), warnings: [] };
}

async function toggleAlert(userId: UserId, alert: AlertKey, enabled: Enabled, expected?: Revision): Promise<SetFlowEnabledResult> {
  const row = await alertsRow(userId);
  if (staleRevision(expected, token(alertState(alert, row).configured))) return TOGGLE_CONFLICT;
  const patched = await patchModule({
    userId,
    name: MOD.alerts,
    isEnabled: keepsModuleOn(enabled, row),
    partial: alertSwitch(alert, enabled),
    expectedRev: row?.revision ?? 0
  });
  if (patched.conflict) return TOGGLE_CONFLICT;
  const after = await alertsRow(userId);
  return { ok: true, enabled, revision: token(alertState(alert, after).configured), warnings: alertWarnings(enabled, row) };
}

function routeToggle(userId: UserId, ref: FlowRef, enabled: Enabled, expected?: Revision): Promise<SetFlowEnabledResult> {
  if (ref.layer === 'automation') return toggleAutomation(userId, ref.id, enabled, expected);
  if (ref.layer === 'command') return toggleCommand(userId, ref.name, enabled, expected);
  return toggleAlert(userId, ref.alert, enabled, expected);
}

async function writeDenial(locals: App.Locals): Promise<FlowDenied | null> {
  const denied = await moduleWriteDenial(locals, moduleDef(MOD.automations)!);
  return denied ? { ok: false, reason: 'denied', error: denied.data.error } : null;
}

export async function setFlowEnabled(locals: App.Locals, ref: FlowRef, enabled: Enabled, expectedRevision?: Revision): Promise<SetFlowEnabledResult> {
  const denied = await writeDenial(locals);
  if (denied) return denied;
  return routeToggle(effectiveId(locals.session), ref, enabled, expectedRevision).catch((err: unknown) => ({
    ok: false as const,
    reason: 'failed' as const,
    error: err instanceof Error ? err.message : String(err)
  }));
}

export async function saveFlow(locals: App.Locals, input: SaveFlowInput): Promise<SaveFlowResult> {
  const denied = await writeDenial(locals);
  if (denied) return denied;
  const issues = validateDefinition(input.definition);
  if (issues.length > 0) return { ok: false, reason: 'invalid', issues };
  return route(effectiveId(locals.session), classifyFlow(input.definition), input).catch(failed);
}

async function removeCommand(userId: UserId, name: CommandName): Promise<DeleteFlowResult> {
  const rows = await ownerCommands(userId);
  if (!rows.some((row) => row.name === name)) return { ok: false, reason: 'not_found' };
  await deleteCommand(userId, name);
  return { ok: true };
}

async function routeDelete(userId: UserId, ref: FlowRef): Promise<DeleteFlowResult> {
  if (ref.layer === 'alert') return { ok: false, reason: 'not_deletable' };
  if (ref.layer === 'command') return removeCommand(userId, ref.name);
  return (await deleteAutomation(userId, ref.id)) ? { ok: true } : { ok: false, reason: 'not_found' };
}

export async function deleteFlow(locals: App.Locals, ref: FlowRef): Promise<DeleteFlowResult> {
  const denied = await writeDenial(locals);
  if (denied) return denied;
  return routeDelete(effectiveId(locals.session), ref).catch((err: unknown) => ({
    ok: false as const,
    reason: 'failed' as const,
    error: err instanceof Error ? err.message : String(err)
  }));
}
