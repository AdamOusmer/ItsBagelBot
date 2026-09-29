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
  BUILTIN_NAMES,
  builtinDef,
  DEFS_PER_BROADCASTER,
  parseJsonPath,
  slugifyName,
  validateFetchDef,
  type FetchDefErrors
} from '@bagel/kit';
import { ValkeyRateLimiter } from '@bagel/kit/server/rate-limit';
import { listCommands, upsertCommand, deleteCommand, listModules, listModulesForEditing, patchModule, type ModuleView } from '$lib/server/commands-store';
import { moduleEditState } from '$lib/server/module-edit-state';
import { saveConflict, conflictField } from '$lib/server/command-conflict';
import { isBulkRequest, runBulk } from '$lib/server/commands-bulk';
import { listFetches, upsertFetchDef, deleteFetchDef } from '$lib/server/fetches-store';
import { saveFetchDef, removeFetchDef, rehearseFetchDef } from '$lib/server/fetch-def-actions';
import { auditDashboardImpersonation, userCommandsPage } from '$lib/server/services';
import { commandsHref } from '@bagel/kit/site-links';
import { logger } from '@bagel/kit/server/logger';
import { actionError, actionErrorBody } from '$lib/server/action-errors';
import { effectiveId } from '$lib/server/board';
import { gateSection } from '$lib/server/module-gate';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

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
    // Editable built-ins carry the saved template (or the default) so the
    // inspector's editor and rehearsal start from the real value.
    response: def.editable ? savedReply || def.preview : def.summary,
    is_active: row ? row.is_enabled : def.defaultActive,
    perm: (PERMS as readonly string[]).includes(savedPerm) ? savedPerm as Perm : def.defaultPerm,
    cooldown: def.defaultCooldown,
    stream_online_only: def.liveOnly,
    builtin: true
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

// mergeCommands lists built-ins first, then the user's custom commands with any
// name colliding with a built-in dropped (built-ins reserve their trigger).
function mergeCommands(custom: CommandView[], modules: ModuleView[]): CommandView[] {
  const builtins = builtinViews(modules);
  const customs = custom.filter((c) => !BUILTIN_NAMES.has(c.name));
  return [...builtins, ...customs];
}

export const load: PageServerLoad = async ({ locals }) => {
  gateSection(locals.session, 'commands');
  const uid = effectiveId(locals.session);
  if (DEMO) {
    const { demoCommandRows, demoFetches } = await import('$lib/server/demo-data');
    return {
      commands: mergeCommands(demoCommandRows, []),
      ...demoFetches(),
      board: uid,
      publicPage: { on: true, url: commandsHref('demo') }
    };
  }
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
  const permRaw = String(f.get('perm') ?? 'everyone');
  const perm: Perm = (PERMS as readonly string[]).includes(permRaw) ? (permRaw as Perm) : 'everyone';

  const cooldown = Math.max(0, Math.floor(Number(f.get('cooldown') ?? 0) || 0));

  const allowedUserId = String(f.get('allowed_user_id') ?? '').replace(/\D/g, '');

  const streamOnlineOnly = f.get('stream_online_only') === 'on';

  const bumpCounter = normName(String(f.get('bump_counter') ?? ''));

  return { name, aliases, response, perm, cooldown, allowedUserId, streamOnlineOnly, bumpCounter };
}

function demoView(cmd: ReturnType<typeof parseCommand>, isActive: boolean): CommandView {
  return {
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId,
    bump_counter: cmd.bumpCounter
  };
}

async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gateSection(locals.session, 'commands');
  if (!DEMO && !locals.session) return null;
  return {
    uid: effectiveId(locals.session),
    session: locals.session,
    locale: locals.locale,
    form: await request.formData()
  };
}

const notSignedIn = (locale: App.Locals['locale']) => fail(401, { ok: false, error: actionError(locale, 'Not signed in.') });

// RPC failure detail can carry internal service information: log it, never return it.
async function tryRpc<T>(action: string, call: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
  try {
    return { ok: true, value: await call() };
  } catch (e) {
    logger.error({ err: e }, `[commands] ${action} failed`);
    return { ok: false };
  }
}

function builtinPermFromForm(f: FormData, defaultPerm: Perm): Perm {
  const value = String(f.get('perm') ?? '');
  return (PERMS as readonly string[]).includes(value) ? value as Perm : defaultPerm;
}

function builtinRow(def: NonNullable<ReturnType<typeof builtinDef>>, response: string, isActive: boolean, perm: Perm = def.defaultPerm): CommandView {
  return { ...builtinView(def), response, is_active: isActive, perm };
}

// Patch only the requested setting against the owning service's latest
// revision. Toggle, reply and access edits can then coexist without replacing
// each other's config, including when two dashboard tabs save at once.
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

// parseSaveForm reads the editor's submission: the shared command fields plus
// the edit/rename bookkeeping. A rename passes original_name so the commands
// service updates the row's name field in place (single write) instead of
// delete-old + create-new.
function parseSaveForm(f: FormData) {
  const cmd = parseCommand(f);
  const isEdit = f.get('edit') === '1';
  const originalName = normName(String(f.get('original_name') ?? ''));
  return {
    cmd,
    isActive: f.get('is_active') === 'on',
    isEdit,
    originalName,
    renamed: isEdit && originalName !== '' && originalName !== cmd.name
  };
}

function saveResult(s: ReturnType<typeof parseSaveForm>, commands: CommandView[]) {
  return {
    ok: true,
    action: s.isEdit ? 'updated' : 'created',
    name: s.cmd.name,
    original: s.renamed ? s.originalName : undefined,
    commands
  };
}

async function guardSave(uid: string, s: ReturnType<typeof parseSaveForm>) {
  const listed = await tryRpc('save-guard', () => listCommands(uid));
  if (!listed.ok) return s.isEdit && !s.renamed ? null : fail(503, { ok: false, code: 'unavailable' });
  const conflict = saveConflict(
    { name: s.cmd.name, aliases: s.cmd.aliases, isEdit: s.isEdit, originalName: s.originalName },
    listed.value
  );
  if (!conflict) return null;
  return fail(409, { ok: false, code: conflict.code, field: conflictField(conflict.code), conflict: conflict.name });
}

function bulkNames(form: FormData): string[] {
  return [...new Set(form.getAll('name').map((n) => normName(String(n))).filter(Boolean))];
}

export const actions: Actions = {
  savefetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const r = await saveFetchDef(ctx.uid, ctx.session, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  },

  deletefetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const r = await removeFetchDef(ctx.uid, ctx.session, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  },

  testfetch: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const r = await rehearseFetchDef(ctx.uid, ctx.form);
    return r.ok ? r.data : fail(r.status, actionErrorBody(ctx.locale, r.body));
  },

  save: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const s = parseSaveForm(ctx.form);

    const errors = validateCommand({
      name: s.cmd.name,
      aliases: s.cmd.aliases,
      response: s.cmd.response,
      cooldown: s.cmd.cooldown,
      allowedUserId: s.cmd.allowedUserId,
      bumpCounter: s.cmd.bumpCounter
    });
    if (Object.keys(errors).length) {
      return fail(400, { ok: false, errors, error: actionError(ctx.locale, firstError(errors) ?? '') });
    }

    if (DEMO) {
      return saveResult(s, [demoView(s.cmd, s.isActive)]);
    }

    const blocked = await guardSave(ctx.uid, s);
    if (blocked) return blocked;

    const res = await tryRpc('save', () =>
      upsertCommand(ctx.uid, { ...s.cmd, isActive: s.isActive }, s.renamed ? s.originalName : undefined)
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, s.isEdit ? 'command:update' : 'command:create', s.cmd.name);
    return saveResult(s, res.value.commands);
  },

  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;

    const cmd = parseCommand(f);
    const isActive = f.get('is_active') === 'on';

    if (DEMO) {
      return { ok: true, action: 'updated', name: cmd.name, commands: [demoView(cmd, isActive)], silent: true };
    }

    const res = await tryRpc('toggle', () => upsertCommand(uid, { ...cmd, isActive }));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:toggle', `${cmd.name}=${isActive}`);

    return { ok: true, action: 'updated', name: cmd.name, commands: res.value.commands, silent: true };
  },

  delete: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;

    const name = String(f.get('name') ?? '');

    if (DEMO) return { ok: true, action: 'deleted', name };

    const res = await tryRpc('delete', () => deleteCommand(uid, name));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:delete', name);

    return { ok: true, action: 'deleted', name, commands: res.value.commands };
  },

  bulk: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const op = ctx.form.get('op');
    const names = bulkNames(ctx.form);
    if (!isBulkRequest(op, names)) {
      return fail(400, { ok: false, error: actionError(ctx.locale, 'Invalid input.') });
    }

    if (DEMO) return { ok: true, op, results: names.map((name) => ({ name, ok: true })) };

    const res = await tryRpc('bulk', () => runBulk(ctx.uid, op, names));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, `command:bulk_${op}`, names.join(','));
    return { ok: true, op, results: res.value };
  },

  toggleBuiltin: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    if (!def) return fail(400, { ok: false, error: actionError(ctx.locale, 'Unknown built-in command.') });
    const isActive = f.get('is_active') === 'on';
    const perm = builtinPermFromForm(f, def.defaultPerm);
    const view = builtinRow(def, String(f.get('response') ?? def.summary), isActive, perm);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('toggleBuiltin', () => patchBuiltin(uid, def, () => ({ isEnabled: isActive, partial: {} })));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_toggle', `${name}=${isActive}`);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  },

  // Save an editable built-in's custom reply template. Like the toggle, the
  // value lives in the modules service (under the built-in id, config key
  // def.replyKey), so this writes there, not the commands service. An empty
  // reply clears the override, so the bot falls back to the default template.
  saveBuiltinReply: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = editableBuiltin(name);
    if (!def) {
      return fail(400, { ok: false, error: actionError(ctx.locale, 'This command has no editable reply.') });
    }
    const reply = String(f.get('reply') ?? '').trim();
    if (reply.length > RESPONSE_MAX) {
      return fail(400, { ok: false, error: actionError(ctx.locale, `Reply is too long (max ${RESPONSE_MAX}).`) });
    }
    const isActive = f.get('is_active') === 'on';
    const perm = builtinPermFromForm(f, def.defaultPerm);
    const view = builtinRow(def, reply || def.preview, isActive, perm);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('saveBuiltinReply', () =>
      patchBuiltin(uid, def, (row) => ({ isEnabled: row?.is_enabled ?? def.defaultActive, partial: { [def.replyKey!]: reply } }))
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_reply', name);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  },

  saveBuiltinAccess: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;
    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    const perm = String(f.get('perm') ?? '');
    if (!def || !(PERMS as readonly string[]).includes(perm)) {
      return fail(400, { ok: false, error: actionError(ctx.locale, 'Invalid built-in command or access level.') });
    }
    const view = builtinRow(def, String(f.get('response') ?? def.summary), f.get('is_active') === 'on', perm as Perm);
    if (DEMO) return { ok: true, action: 'updated', name, commands: [view], silent: true };

    const res = await tryRpc('saveBuiltinAccess', () =>
      patchBuiltin(uid, def, (row) => ({ isEnabled: row?.is_enabled ?? def.defaultActive, partial: { permission: perm } }))
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_access', `${name}=${perm}`);
    return { ok: true, action: 'updated', name, commands: [res.value], silent: true };
  }
};

function editableBuiltin(name: string) {
  const def = builtinDef(name);
  if (!def?.editable || !def.replyKey) return undefined;
  return def;
}
