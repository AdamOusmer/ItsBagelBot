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
import { listCommands, upsertCommand, deleteCommand, listModules, upsertModule, type ModuleView } from '$lib/server/commands-store';
import { listFetches, upsertFetchDef, deleteFetchDef } from '$lib/server/fetches-store';
import { saveFetchDef, removeFetchDef, rehearseFetchDef } from '$lib/server/fetch-def-actions';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import type { Session } from '$lib/server/session';
import { actionError, actionErrorBody } from '$lib/server/action-errors';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

function gateCommands(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('commands')) {
    throw redirect(302, '/');
  }
}

function configString(configs: unknown, key: string): string {
  if (configs && typeof configs === 'object') {
    const v = (configs as Record<string, unknown>)[key];
    if (typeof v === 'string') return v;
  }
  return '';
}

function builtinViews(modules: ModuleView[]): CommandView[] {
  const byName = new Map(modules.map((m) => [m.name, m]));
  return BUILTIN_COMMANDS.map((def) => {
    const row = byName.get(def.id);
    const savedReply = def.editable && def.replyKey ? configString(row?.configs, def.replyKey) : '';
    return {
      name: def.id,
      aliases: def.aliases,
      response: def.editable ? savedReply || def.preview : def.summary,
      is_active: row ? row.is_enabled : def.defaultActive,
      perm: def.defaultPerm,
      cooldown: def.defaultCooldown,
      stream_online_only: def.liveOnly,
      builtin: true
    } satisfies CommandView;
  });
}

function mergeCommands(custom: CommandView[], modules: ModuleView[]): CommandView[] {
  const builtins = builtinViews(modules);
  const customs = custom.filter((c) => !BUILTIN_NAMES.has(c.name));
  return [...builtins, ...customs];
}

export const load: PageServerLoad = async ({ locals }) => {
  gateCommands(locals.session);
  const uid = effectiveId(locals.session);
  if (DEMO) {
    const { demoCommandRows, demoFetches } = await import('$lib/server/demo-data');
    return { commands: mergeCommands(demoCommandRows, []), ...demoFetches() };
  }
  try {
    const [custom, modules, fetches] = await Promise.all([
      listCommands(uid),
      listModules(uid).catch(() => []),
      listFetches(uid).catch(() => ({ defs: [], keys: [] }))
    ]);
    return { commands: mergeCommands(custom, modules), ...fetches };
  } catch {
    return { commands: mergeCommands([], []), defs: [], keys: [], degraded: true };
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
  gateCommands(locals.session);
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

function builtinRow(def: NonNullable<ReturnType<typeof builtinDef>>, response: string, isActive: boolean): CommandView {
  return {
    name: def.id,
    aliases: def.aliases,
    response,
    is_active: isActive,
    perm: def.defaultPerm,
    cooldown: def.defaultCooldown,
    stream_online_only: def.liveOnly,
    builtin: true
  };
}

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

  toggleBuiltin: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn(event.locals.locale);
    const { uid, form: f } = ctx;

    const name = normName(String(f.get('name') ?? ''));
    const def = builtinDef(name);
    if (!def) return fail(400, { ok: false, error: actionError(ctx.locale, 'Unknown built-in command.') });
    const isActive = f.get('is_active') === 'on';
    const view = builtinRow(def, def.summary, isActive);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('toggleBuiltin', () => upsertModule(uid, def.id, isActive));
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_toggle', `${name}=${isActive}`);
    return { ok: true, action: 'updated', name, commands: [view], silent: true };
  },

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
    const view = builtinRow(def, reply || def.preview, isActive);

    if (DEMO) {
      return { ok: true, action: 'updated', name, commands: [view], silent: true };
    }

    const res = await tryRpc('saveBuiltinReply', () =>
      upsertModule(uid, def.id, isActive, reply ? { [def.replyKey!]: reply } : undefined)
    );
    if (!res.ok) return fail(400, { ok: false });

    auditDashboardImpersonation(ctx.session, 'command:builtin_reply', name);
    return { ok: true, action: 'updated', name, commands: [view], silent: true };
  }
};

function editableBuiltin(name: string) {
  const def = builtinDef(name);
  if (!def?.editable || !def.replyKey) return undefined;
  return def;
}
