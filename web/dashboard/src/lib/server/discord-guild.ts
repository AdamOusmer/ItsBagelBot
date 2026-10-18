// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, RequestEvent, ServerLoadEvent } from '@sveltejs/kit';
import {
  blankDiscordConfig,
  blankLayout,
  blankStatus,
  botStatus,
  guildLayout,
  listGuildModules,
  listGuilds,
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  readLegacyBlob,
  repostDesk,
  saveDiscordModule,
  saveGuildConfig,
  setGuildModule,
  setupGuild,
  unbindGuild,
  type DiscordConfig,
  type DiscordGuildSummary,
  type DiscordGuildTarget,
  type DiscordLayout,
  type DiscordModuleRow,
  type DiscordStatus
} from '$lib/server/discord-store';
import { discordConfigured, requireDiscordActor } from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/kit/server/logger';
import { assertModuleUnlocked, gateModulePage, moduleLocked } from '$lib/server/module-gate';
import { DISCORD_DEF } from '$lib/server/discord-def';
import {
  DISCORD_CONFIG_VERSION_NEW,
  alertOff,
  alertOn,
  clearPinnedSlots,
  droppedPinNotice,
  discordModuleById,
  discordModuleToggleable,
  legacyConfigFor,
  mergeDiscordConfig,
  parseDiscordModuleSettings,
  validateDiscordModule,
  type DiscordModuleDef,
  type DiscordModuleSettings,
  type PinnedSlot
} from '@bagel/kit';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { error, fail, isRedirect, redirect, type Cookies } from '@sveltejs/kit';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

export type DiscordGuildPage = {
  locked: boolean;
  enabled: boolean;
  guildId: string;
  guilds: DiscordGuildSummary[];
  config: DiscordConfig;
  version: number;
  found: boolean;
  layout: DiscordLayout;
  status: DiscordStatus;
  configured: boolean;
  justConnected: boolean;
  refused: boolean;
  droppedPins: PinnedSlot[];
  degraded: boolean;
};

function blankPage(guildId: string, locked: boolean): DiscordGuildPage {
  return {
    locked,
    enabled: false,
    guildId,
    guilds: [],
    config: { ...blankDiscordConfig(), guildId },
    version: 0,
    found: false,
    layout: blankLayout(),
    status: blankStatus(),
    configured: discordConfigured(),
    justConnected: false,
    refused: false,
    droppedPins: [],
    degraded: false
  };
}

/** 404, not 403: a 403 would confirm the guild exists and belongs to someone. */
function ownedGuild(guilds: DiscordGuildSummary[], guildId: string): DiscordGuildSummary {
  const found = guilds.find((g) => g.guildId === guildId);
  if (!found) throw error(404, 'No such server.');
  return found;
}

const DROPPED_COOKIE = 'bb_discord_dropped';

function droppedPath(guildId: string): string {
  return `/discord/${guildId}`;
}

function rememberDroppedPins(cookies: Cookies, guildId: string, slots: PinnedSlot[]): void {
  if (slots.length === 0) return;
  cookies.set(DROPPED_COOKIE, slots.join(','), {
    path: droppedPath(guildId),
    httpOnly: true,
    sameSite: 'lax',
    maxAge: 300
  });
}

function takeDroppedPins(cookies: Cookies, guildId: string): PinnedSlot[] {
  const raw = cookies.get(DROPPED_COOKIE);
  if (!raw) return [];
  cookies.delete(DROPPED_COOKIE, { path: droppedPath(guildId) });
  return droppedPinNotice(raw.split(','));
}

export const loadGuildShell = async ({
  cookies,
  locals,
  params,
  url
}: ServerLoadEvent): Promise<DiscordGuildPage> => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const guildId = params.guildId ?? '';
  const locked = await moduleLocked(locals, DISCORD_DEF);
  const droppedPins = takeDroppedPins(cookies, guildId);

  if (DEMO) return { ...(await demoPage(guildId, url)), droppedPins };

  const view = await readDiscord({ userId: uid }).catch(() => null);
  if (!view) return { ...blankPage(guildId, locked), degraded: true };
  ownedGuild(view.guilds, guildId);

  const target = { userId: uid, guildId };
  const flags = {
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
  const reads = await guildReads(target);
  return {
    ...blankPage(guildId, locked),
    ...flags,
    droppedPins,
    enabled: view.enabled,
    guilds: view.guilds,
    ...reads,
    ...(await migrateLegacy(target, view.twitchLogin, view.enabled, reads))
  };
};

export type DiscordModulePage = {
  locked: boolean;
  guildId: string;
  def: DiscordModuleDef;
  enabled: boolean;
  settings: DiscordModuleSettings;
  version: number;
  found: boolean;
  guilds: DiscordGuildSummary[];
  layout: DiscordLayout;
  status: DiscordStatus;
  configured: boolean;
  degraded: boolean;
};

function moduleDef(moduleId: string): DiscordModuleDef {
  const def = discordModuleById(moduleId);
  if (!def) throw error(404, 'No such module.');
  return def;
}

function defaultRow(def: DiscordModuleDef): DiscordModuleRow {
  return {
    enabled: def.kind !== 'optin',
    settings: parseDiscordModuleSettings(def.id, null),
    version: 0
  };
}

async function moduleRead(
  target: DiscordGuildTarget,
  def: DiscordModuleDef
): Promise<{ row: DiscordModuleRow; found: boolean; degraded: boolean }> {
  try {
    const list = await listGuildModules(target);
    return { row: list.modules[def.id] ?? defaultRow(def), found: list.found, degraded: false };
  } catch (e) {
    logger.warn({ err: e }, '[discord] guild modules unavailable');
    return { row: defaultRow(def), found: false, degraded: true };
  }
}

async function demoModulePage(def: DiscordModuleDef, guildId: string, url: URL): Promise<DiscordModulePage> {
  const page = await demoPage(guildId, url);
  const row = defaultRow(def);
  const settings = parseDiscordModuleSettings(def.id, page.config);
  return {
    locked: false,
    guildId,
    def,
    enabled: row.enabled,
    settings,
    version: page.version,
    found: page.found,
    guilds: page.guilds,
    layout: page.layout,
    status: page.status,
    configured: page.configured,
    degraded: false
  };
}

export const loadGuildModule = async (
  { locals, params, url }: ServerLoadEvent,
  moduleId: string
): Promise<DiscordModulePage> => {
  gate(locals.session);
  const def = moduleDef(moduleId);
  const guildId = params.guildId ?? '';
  const locked = await moduleLocked(locals, DISCORD_DEF);
  if (DEMO) return demoModulePage(def, guildId, url);

  const target = { userId: effectiveId(locals.session), guildId };
  const view = await readDiscord({ userId: target.userId }).catch(() => null);
  const base = { locked, guildId, def, configured: discordConfigured() };
  if (!view) {
    return { ...base, ...defaultRow(def), found: false, guilds: [], layout: blankLayout(), status: blankStatus(), degraded: true };
  }
  ownedGuild(view.guilds, guildId);
  const [read, layout, status] = await Promise.all([moduleRead(target, def), loadLayout(target), loadStatus(target)]);
  return {
    ...base,
    ...read.row,
    found: read.found,
    guilds: view.guilds,
    layout,
    status,
    degraded: read.degraded
  };
};

async function migrateLegacy(
  target: DiscordGuildTarget,
  twitchLogin: string,
  enabled: boolean,
  reads: { config: DiscordConfig; version: number; found: boolean; degraded: boolean }
): Promise<{ config: DiscordConfig; version: number } | null> {
  if (reads.found || reads.degraded) return null;
  try {
    const legacy = legacyConfigFor(await readLegacyBlob({ userId: target.userId }), target.guildId);
    if (!legacy) return null;
    const config = { ...legacy, twitchLogin: twitchLogin || legacy.twitchLogin };
    const saved = await saveGuildConfig({ ...target, config, expectedVersion: DISCORD_CONFIG_VERSION_NEW });
    if (saved.error || saved.code) {
      logger.warn({ code: saved.code }, '[discord] legacy config migration refused');
      return null;
    }
    await saveDiscordModule({ userId: target.userId, enabled, twitchLogin });
    logger.info({ guildId: target.guildId }, '[discord] migrated the legacy module blob onto the guild row');
    return { config, version: saved.version };
  } catch (e) {
    logger.warn({ err: e }, '[discord] legacy config migration failed');
    return null;
  }
}

async function demoPage(guildId: string, url: URL): Promise<DiscordGuildPage> {
  const { demoDiscordView, demoDiscordConfig, demoDiscordLayout, demoDiscordStatus } = await import(
    '$lib/server/demo-data'
  );
  const view = demoDiscordView();
  const g = ownedGuild(view.guilds, guildId);
  const row = demoDiscordConfig();
  const guild = { id: guildId, name: g.name, iconUrl: g.iconUrl, memberCount: g.memberCount };
  return {
    ...blankPage(guildId, false),
    enabled: view.enabled,
    guilds: view.guilds,
    config: { ...row.config, guildId },
    version: row.version,
    found: row.found,
    layout: { ...demoDiscordLayout(), guild, botOnline: g.botPresent, needsReauth: g.needsReauth },
    status: {
      ...demoDiscordStatus(),
      guild,
      online: g.botPresent,
      guildPresent: g.botPresent,
      needsReauth: g.needsReauth
    },
    configured: true,
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
}

async function demoOutcome(label: string, ctx: ActionCtx) {
  if (label === 'save') return demoSave(ctx);
  if (label === 'setup') {
    const { demoDiscordDroppedPins } = await import('$lib/server/demo-data');
    rememberDroppedPins(ctx.cookies, ctx.guildId, droppedPinNotice(demoDiscordDroppedPins()));
  }
  return { ok: true };
}

async function demoSave(ctx: ActionCtx) {
  const { demoDiscordConfig, demoDiscordRefusedField } = await import('$lib/server/demo-data');
  const stored = demoDiscordConfig().config;
  const draft = parseDraft(ctx.form.get('config'));
  const { errors } = mergeDiscordConfig(stored, draft);
  const refused = demoDiscordRefusedField();
  const fields = errors.map((e) => String(e.field));
  if (draft[refused] !== stored[refused]) fields.push(refused);
  if (fields.length === 0) return { ok: true };
  return fail(400, {
    ok: false,
    code: 'invalid',
    error: `Some settings were not valid: ${fields.join(', ')}.`,
    fields
  });
}

type GuildReads = {
  config: DiscordConfig;
  version: number;
  found: boolean;
  layout: DiscordLayout;
  status: DiscordStatus;
  degraded: boolean;
};

async function guildReads(target: DiscordGuildTarget): Promise<GuildReads> {
  const [row, layout, status] = await Promise.all([
    readGuildConfig(target).catch((e) => {
      logger.warn({ err: e }, '[discord] guild config unavailable');
      return null;
    }),
    loadLayout(target),
    loadStatus(target)
  ]);
  if (!row) {
    return {
      config: { ...blankDiscordConfig(), guildId: target.guildId },
      version: 0,
      found: false,
      layout,
      status,
      degraded: true
    };
  }
  return { config: row.config, version: row.version, found: row.found, layout, status, degraded: false };
}

async function loadLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  try {
    return await guildLayout(target);
  } catch (e) {
    logger.warn({ err: e }, '[discord] layout unavailable');
    return blankLayout();
  }
}

async function loadStatus(target: DiscordGuildTarget): Promise<DiscordStatus> {
  try {
    return await botStatus(target);
  } catch (e) {
    logger.warn({ err: e }, '[discord] status unavailable');
    return { ...blankStatus(), code: 'discord_unavailable' };
  }
}

type ActionCtx = {
  uid: string;
  guildId: string;
  target: DiscordGuildTarget;
  session: Session | null | undefined;
  locals: App.Locals;
  cookies: Cookies;
  form: FormData;
  moduleId: string;
};

async function actionContext({ cookies, request, locals, params }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  const uid = effectiveId(locals.session);
  const guildId = params.guildId ?? '';
  return {
    uid,
    guildId,
    target: { userId: uid, guildId },
    session: locals.session,
    locals,
    cookies,
    form: await request.formData(),
    moduleId: params.id ?? ''
  };
}

type Outcome<T> = { ok: true; data: T } | { ok: false; error: string };

type ActionWork = { label: string; failMsg: string };

async function attempt<T>(work: ActionWork, run: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, data: await run() };
  } catch (e) {
    if (isRedirect(e)) throw e;
    logger.error({ err: e }, `[discord] ${work.label} failed`);
    return { ok: false, error: work.failMsg };
  }
}

type Refusal = { error: string; code?: string; fields?: string[] };

function refusalOf(data: Record<string, unknown>): Refusal | null {
  const error = typeof data.error === 'string' ? data.error : '';
  const code = typeof data.code === 'string' ? data.code : '';
  if (!error && !code) return null;
  const fields = Array.isArray(data.fields) ? (data.fields as string[]) : undefined;
  return { error, code, ...(fields ? { fields } : {}) };
}

// Checked per action too: the load's ownership check does not run for a form POST.
async function assertOwned(ctx: ActionCtx): Promise<void> {
  if (DEMO) return;
  ownedGuild(await listGuilds({ userId: ctx.uid }), ctx.guildId);
}

function discordAction<T extends Record<string, unknown>>(
  work: ActionWork,
  run: (ctx: ActionCtx) => Promise<T | Refusal>
) {
  return async (event: RequestEvent) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: 'Not signed in.' });
    if (!(await assertModuleUnlocked(event.locals, DISCORD_DEF))) {
      return fail(403, { ok: false, code: 'locked', error: 'Discord is in beta and open to Premium channels only.' });
    }
    if (DEMO) return demoOutcome(work.label, ctx);
    const r = await attempt(work, async () => {
      await assertOwned(ctx);
      return run(ctx);
    });
    if (!r.ok) return fail(400, { ok: false, error: r.error });
    const refusal = refusalOf(r.data);
    if (refusal) return fail(400, { ok: false, ...refusal });
    return { ok: true, ...r.data };
  };
}

function parseDraft(raw: FormDataEntryValue | null): Record<string, unknown> {
  if (typeof raw !== 'string') return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed === null) return {};
    if (typeof parsed !== 'object') return {};
    if (Array.isArray(parsed)) return {};
    return parsed as Record<string, unknown>;
  } catch {
    return {};
  }
}

/** 0 for missing or unparseable: outgress refuses 0 on an existing row instead of clobbering it. */
function parseVersion(raw: FormDataEntryValue | null): number {
  if (typeof raw !== 'string') return 0;
  const n = Number.parseInt(raw.trim(), 10);
  return Number.isSafeInteger(n) && n >= 0 ? n : 0;
}

function fieldNames(errors: { field: string }[]): string {
  return errors.map((e) => e.field).join(', ');
}

const FLAG_ON = new Set(['on', 'true', '1']);

function activeModule(ctx: ActionCtx): DiscordModuleDef | Refusal {
  const def = discordModuleById(ctx.moduleId);
  return def ?? { error: 'No such module.', code: 'invalid' };
}

async function currentRow(ctx: ActionCtx, def: DiscordModuleDef): Promise<DiscordModuleRow> {
  const list = await listGuildModules(ctx.target);
  return list.modules[def.id] ?? defaultRow(def);
}

function moduleFailure(result: { error: string; code: string; fields: string[] }): Refusal {
  return { error: result.error, code: result.code, ...(result.fields.length ? { fields: result.fields } : {}) };
}

export const moduleActions: Actions = {
  save: discordAction({ label: 'module.save', failMsg: 'Could not save these settings.' }, async (ctx) => {
    const def = activeModule(ctx);
    if ('error' in def) return def;
    const row = await currentRow(ctx, def);
    const checked = validateDiscordModule(def.id, parseDraft(ctx.form.get('settings')), row.settings);
    if (!checked) return { error: 'No such module.', code: 'invalid' };
    if (checked.errors.length) {
      return {
        error: `Some settings were not valid: ${fieldNames(checked.errors)}.`,
        code: 'invalid',
        fields: checked.errors.map((e) => e.field)
      };
    }
    const result = await setGuildModule({
      ...ctx.target,
      name: def.id,
      enabled: row.enabled,
      settings: checked.settings,
      expectedVersion: parseVersion(ctx.form.get('version'))
    });
    if (result.error || result.code) return moduleFailure(result);
    auditDashboardImpersonation(ctx.session, `discord:module.save:${def.id}`, ctx.guildId);
    return { version: result.version };
  }),

  toggle: discordAction({ label: 'module.toggle', failMsg: 'Could not change this module.' }, async (ctx) => {
    const def = activeModule(ctx);
    if ('error' in def) return def;
    if (!discordModuleToggleable(def)) return { error: 'This module is always on.', code: 'invalid' };
    const row = await currentRow(ctx, def);
    const enabled = FLAG_ON.has(String(ctx.form.get('enabled') ?? '').trim());
    const result = await setGuildModule({
      ...ctx.target,
      name: def.id,
      enabled,
      settings: row.settings,
      expectedVersion: parseVersion(ctx.form.get('version'))
    });
    if (result.error || result.code) return moduleFailure(result);
    auditDashboardImpersonation(ctx.session, `discord:module.toggle:${def.id}`, ctx.guildId);
    return { version: result.version, enabled };
  })
};

export const guildActions: Actions = {
  save: discordAction({ label: 'save', failMsg: 'Could not save Discord settings.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    const { config, errors } = mergeDiscordConfig(row.config, parseDraft(ctx.form.get('config')));
    const result = await saveGuildConfig({
      ...ctx.target,
      config,
      expectedVersion: parseVersion(ctx.form.get('version'))
    });
    if (result.error || result.code) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:save', ctx.guildId);
    if (errors.length) {
      return { error: `Some settings were not valid: ${fieldNames(errors)}.`, code: 'invalid', fields: errors.map((e) => e.field) };
    }
    return { version: result.version };
  }),

  setup: discordAction({ label: 'setup', failMsg: 'Could not set up this server.' }, async (ctx) => {
    requireDiscordActor(ctx.locals);
    const view = await readDiscord({ userId: ctx.uid });
    const row = await readGuildConfig(ctx.target);
    const login = ctx.session?.login ?? view.twitchLogin ?? row.config.twitchLogin;
    const result = await setupGuild(
      {
        ...ctx.target,
        installedBy: ctx.session?.user_id ?? ctx.uid,
        subscribers: alertOff(row.config.subscribersEnabled),
        pinnedRoles: pinnedRolesOf(row.config)
      },
      { ...row.config, twitchLogin: login }
    );
    if (result.error) return { error: result.error, code: result.code };
    const config = clearPinnedSlots({ ...result.config, twitchLogin: login }, result.droppedPins);
    const saved = await persistSetup(ctx.target, config);
    if (saved.error || saved.code) return { error: saved.error, code: saved.code };
    auditDashboardImpersonation(ctx.session, 'discord:setup', ctx.guildId);
    rememberDroppedPins(ctx.cookies, ctx.guildId, result.droppedPins);
    return { refused: result.refused };
  }),

  repost: discordAction({ label: 'repost', failMsg: 'Could not repost the ticket panel.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    if (!alertOn(row.config.ticketsEnabled)) {
      return { error: 'Turn the ticket desk on first.', code: 'tickets_off' };
    }
    const result = await repostDesk(ctx.target, row.config);
    if (result.error) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:repost', ctx.guildId);
    return { messageId: result.messageId };
  }),

  disconnect: async (event: RequestEvent) => {
    const run = discordAction({ label: 'disconnect', failMsg: 'Could not disconnect Discord.' }, async (ctx) => {
      await unbindGuild(ctx.target);
      auditDashboardImpersonation(ctx.session, 'discord:disconnect', ctx.guildId);
      return {};
    });
    const result = await run(event);
    if ('ok' in result && result.ok === true) throw redirect(303, '/discord');
    return result;
  }
};
