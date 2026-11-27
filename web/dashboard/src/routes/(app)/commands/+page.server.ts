// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { CommandView, Perm } from '@bagel/kit';
import {
  PERMS,
  RESPONSE_MAX,
  normName,
  normalizeCommandResponse,
  validateCommand,
  firstError,
  BUILTIN_COMMANDS,
  builtinDef,
  DEFS_PER_BROADCASTER,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type FetchDefErrors
} from '@bagel/kit';
import { ValkeyRateLimiter } from '@bagel/kit/server/rate-limit';
import { listCommands, upsertCommand, deleteCommand, listModules, listModulesForEditing, patchModule, type ModuleView } from '#lib/server/commands-store';
import { moduleEditState } from '#lib/server/module-edit-state';
import { saveConflict, conflictField } from '#lib/server/command-conflict';
import { recipeTimeLeftNames } from '#lib/server/recipe-time-left';
import { isBulkRequest, runBulk } from '#lib/server/commands-bulk';
import { listFetches, upsertFetchDef, deleteFetchDef } from '#lib/server/fetches-store';
import { saveFetchDef, removeFetchDef, rehearseFetchDef } from '#lib/server/fetch-def-actions';
import { auditDashboardImpersonation, userCommandsPage } from '#lib/server/services';
import { commandsHref } from '@bagel/kit/site-links';
import { logger } from '@bagel/kit/server/logger';
import { actionError, actionErrorBody } from '#lib/server/action-errors';
import { effectiveId } from '#lib/server/board';
import { parsePerm, signedInAction } from '#lib/server/action-context';
import { accountPremium, gateSection } from '#lib/server/module-gate';
import { fail } from '@sveltejs/kit';

function configString(configs: unknown, key: string): string {
  if (configs && typeof configs === 'object') {
    const v = (configs as Record<string, unknown>)[key];
    if (typeof v === 'string') return v;
  }
  return '';
}

function builtinView(def: (typeof BUILTIN_COMMANDS)[number], row?: ModuleView): CommandView {
  const savedReply = def.editable && def.replyKey ? configString(row?.configs, def.replyKey) : '';
  const savedPerm = configString(row?.configs, 'permission');
  return {
    name: def.id,
    aliases: def.aliases,
    response: def.editable ? savedReply || def.preview : def.summary,
    is_active: row ? row.is_enabled : def.defaultActive,
    perm: parseKitPerm(savedPerm, def.defaultPerm),
    cooldown: def.defaultCooldown,
    stream_online_only: def.liveOnly,
    builtin: true,
    premium: def.premium
  } satisfies CommandView;
}

async function publicPageState(session: App.Locals['session'], uid: string) {
  const login = (session?.delegate_of ? session.delegate_login : session?.login) ?? '';
  const on = await userCommandsPage(uid).catch(() => true);
  return { on, url: login ? commandsHref(login.toLowerCase()) : '' };
}

function builtinViews(modules: ModuleView[]): CommandView[] {
  const byName = new Map(modules.map((m) => [m.name, m]));
  return BUILTIN_COMMANDS.map((def) => builtinView(def, byName.get(def.id)));
}

function yieldsTo(custom: CommandView[], view: CommandView): boolean {
  if (view.is_active || builtinDef(view.name)?.yields !== true) return false;
  return custom.some((c) => c.name === view.name);
}

function mergeCommands(custom: CommandView[], modules: ModuleView[]): CommandView[] {
  const builtins = builtinViews(modules).filter((view) => !yieldsTo(custom, view));
  const shown = new Set(builtins.map((view) => view.name));
  const customs = custom.filter((c) => !shown.has(c.name));
  return [...builtins, ...customs];
}

export const load: PageServerLoad = async ({ locals, depends }) => {
  depends('app:commands');
  gateSection(locals.session, 'commands');
  const uid = effectiveId(locals.session);
  try {
    const [custom, modules, fetches, publicPage] = await Promise.all([
      listCommands(uid),
      listModules(uid).catch(() => []),
      listFetches(uid).catch(() => ({ defs: [], keys: [] })),
      publicPageState(locals.session, uid)
    ]);
    return { commands: mergeCommands(custom, modules), ...fetches, board: uid, publicPage };
  } catch {
    return { commands: mergeCommands([], []), defs: [], keys: [], board: uid, degraded: true, publicPage: null };
  }
};

function parseCommand(f: FormData) {
  const name = normName(String(f.get('name') ?? ''));

  const seen = new Set<string>();
  const aliases: string[] = [];
  for (const raw of f.getAll('aliases')) {
    const a = normName(String(raw));
    if (!a) continue;
    if (seen.has(a)) continue;
    seen.add(a);
    aliases.push(a);
  }

  const response = normalizeCommandResponse(String(f.get('response') ?? ''));
  const perm = parseKitPerm(f.get('perm'), 'everyone' as Perm);

  const cooldown = seconds(f.get('cooldown'));
  const userCooldown = seconds(f.get('user_cooldown'));

  const allowedUserId = String(f.get('allowed_user_id') ?? '').trim();

  const streamOnlineOnly = f.get('stream_online_only') === 'on';

  const bumpCounter = normName(String(f.get('bump_counter') ?? ''));

  return { name, aliases, response, perm, cooldown, userCooldown, allowedUserId, streamOnlineOnly, bumpCounter };
}

function seconds(raw: FormDataEntryValue | null): number {
  return Math.max(0, Math.floor(Number(raw ?? 0) || 0));
}

const commandsAction = signedInAction((session) => gateSection(session, 'commands'));

// RPC failure detail can carry internal service information: log it, never return it.
async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (e) {
    logger.error({ err: e }, `[commands] ${action} failed`);
    return { ok: false };
  }
}

function parseKitPerm<F>(raw: unknown, fallback: F): Perm | F {
  return parsePerm(raw, PERMS, fallback);
}

async function patchBuiltin(
  uid: string,
  def: NonNullable<ReturnType<typeof builtinDef>>,
  change: (row?: ModuleView) => { isEnabled: boolean; partial: Record<string, string> }
): Promise<CommandView> {
  for (let attempt = 0; attempt < 4; attempt++) {
    const row = (await listModulesForEditing(uid)).find((m) => m.name === def.id);
    const { config, revision } = moduleEditState(row);
    const { isEnabled, partial } = change(row);
    const result = await patchModule({ userId: uid, name: def.id, isEnabled, partial, expectedRev: revision });
    if (!result.conflict) {
      return builtinView(def, { name: def.id, is_enabled: isEnabled, configs: { ...config, ...partial }, revision: result.rev });
    }
  }
  throw new Error(`Built-in command ${def.id} changed during save`);
}

function parseRestoreUses(raw: FormDataEntryValue | null): number {
  const n = Math.floor(Number(raw ?? 0));
  return Number.isSafeInteger(n) && n > 0 ? n : 0;
}

function parseSaveForm(f: FormData) {
  const cmd = parseCommand(f);
  const isEdit = f.get('edit') === '1';
  const originalName = normName(String(f.get('original_name') ?? ''));
  return {
    cmd,
    restoreUses: isEdit ? 0 : parseRestoreUses(f.get('restore_uses')),
    isActive: f.get('is_active') === 'on',
    isEdit,
    originalName,
    renamed: isEdit && originalName !== '' && originalName !== cmd.name
  };
}

function saveResult(s: ReturnType<typeof parseSaveForm>, commands: CommandView[], restored = false) {
  return {
    ok: true,
    action: s.isEdit ? 'updated' : 'created',
    name: s.cmd.name,
    original: s.renamed ? s.originalName : undefined,
    restored,
    commands
  };
}

async function guardSave(uid: string, s: ReturnType<typeof parseSaveForm>) {
  const listed = await tryRpc('save-guard', () => Promise.all([listCommands(uid), recipeTimeLeftNames(uid)]));
  if (!listed.ok) return s.isEdit && !s.renamed ? null : fail(503, { ok: false, code: 'unavailable' });
  const conflict = saveConflict(
    { name: s.cmd.name, aliases: s.cmd.aliases, isEdit: s.isEdit, originalName: s.originalName },
    listed.value[0],
    listed.value[1]
  );
  if (!conflict) return null;
  return fail(409, { ok: false, code: conflict.code, field: conflictField(conflict.code), conflict: conflict.name });
}

function bulkNames(form: FormData): string[] {
  return [...new Set(form.getAll('name').map((n) => normName(String(n))).filter(Boolean))];
}

export const actions: Actions = {
  savefetch: commandsAction(async (ctx) => {
    const r = await saveFetchDef(ctx.uid, ctx.session, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  }),

  deletefetch: commandsAction(async (ctx) => {
    const r = await removeFetchDef(ctx.uid, ctx.session, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  }),

  testfetch: commandsAction(async (ctx) => {
    const r = await rehearseFetchDef(ctx.uid, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  }),

  save: commandsAction(async (ctx) => {
    const s = parseSaveForm(ctx.form);

    const errors = validateCommand({
      name: s.cmd.name,
      aliases: s.cmd.aliases,
      response: s.cmd.response,
      cooldown: s.cmd.cooldown,
      userCooldown: s.cmd.userCooldown,
      allowedUserId: s.cmd.allowedUserId,
      bumpCounter: s.cmd.bumpCounter
    });
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: actionError(ctx.locale, firstError(errors) ?? '') });
    }

    const blocked = await guardSave(ctx.uid, s);
    if (blocked) return blocked;

    const res = await tryRpc('save', () =>
      upsertCommand(
        ctx.uid,
        { ...s.cmd, isActive: s.isActive, restoreUses: s.restoreUses },
        s.renamed ? s.originalName : undefined
      )
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, s.isEdit ? 'command:update' : 'command:create', s.cmd.name);
    return saveResult(s, res.value.commands, res.value.restored);
  }),

  toggle: commandsAction(async (ctx) => {
    const { uid, form: f } = ctx;

    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    const res = await tryRpc('toggle', () => upsertCommand(uid, { ...cmd, isActive }));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:toggle', `${cmd.name}=${isActive}`);

    return { ok: true, action: 'updated', name: cmd.name, commands: res.value.commands, silent: true };
  }),

  delete: commandsAction(async (ctx) => {
    const { uid, form: f } = ctx;

    const name = String(f.get('name') ?? '');

    const res = await tryRpc('delete', () => deleteCommand(uid, name));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:delete', name);

    return { ok: true, action: 'deleted', name, commands: res.value.commands };
  }),

  bulk: commandsAction(async (ctx) => {
    const op = ctx.form.get('op');
    const names = bulkNames(ctx.form);
    if (!isBulkRequest(op, names)) {
      return fail(400, { ok: false, error: actionError(ctx.locale, 'Invalid input.') });
    }

    const res = await tryRpc('bulk', () => runBulk(ctx.uid, op, names));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, `command:bulk_${op}`, names.join(','));
    return { ok: true, op, results: res.value };
  }),

  toggleBuiltin: commandsAction(async (ctx) => {
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    if (!def) return fail(400, { ok: false, error: actionError(ctx.locale, 'Unknown built-in command.') });
    const isActive = f.get('is_active') === 'on';
    const needsPremium = def.premium === true && isActive;
    if (needsPremium && !(await accountPremium(ctx.uid))) {
      return fail(403, { ok: false, error: actionError(ctx.locale, 'Premium only.') });
    }

    const res = await tryRpc('toggleBuiltin', () => patchBuiltin(uid, def, () => ({ isEnabled: isActive, partial: {} })));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_toggle', `${name}=${isActive}`);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  }),

  saveBuiltinReply: commandsAction(async (ctx) => {
    const parsed = parseBuiltinReply(ctx.form, ctx.locale);
    if ('refusal' in parsed) return parsed.refusal;
    const { name, def, reply } = parsed;

    const res = await tryRpc('saveBuiltinReply', () =>
      patchBuiltin(ctx.uid, def, (row) => ({ isEnabled: row?.is_enabled ?? def.defaultActive, partial: { [def.replyKey]: reply } }))
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_reply', name);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  }),

  saveBuiltinAccess: commandsAction(async (ctx) => {
    const { uid, form: f } = ctx;
    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    const perm = parseKitPerm(f.get('perm'), null);
    if (!def || !perm) {
      return fail(400, { ok: false, error: actionError(ctx.locale, 'Invalid built-in command or access level.') });
    }
    const res = await tryRpc('saveBuiltinAccess', () =>
      patchBuiltin(uid, def, (row) => ({ isEnabled: row?.is_enabled ?? def.defaultActive, partial: { permission: perm } }))
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_access', `${name}=${perm}`);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  })
};

function editableBuiltin(name: string) {
  const def = builtinDef(name);
  if (!def?.editable || !def.replyKey) return undefined;
  return def as typeof def & { replyKey: string };
}

function parseBuiltinReply(f: FormData, locale: App.Locals['locale']) {
  const name = normName(String(f.get('name') ?? ''));
  const def = editableBuiltin(name);
  if (!def) {
    return { refusal: fail(400, { ok: false, error: actionError(locale, 'This command has no editable reply.') }) };
  }
  const reply = String(f.get('reply') ?? '').trim();
  if (reply.length > RESPONSE_MAX) {
    return { refusal: fail(400, { ok: false, error: actionError(locale, `Reply is too long (max ${RESPONSE_MAX}).`) }) };
  }
  return { name, def, reply };
}
