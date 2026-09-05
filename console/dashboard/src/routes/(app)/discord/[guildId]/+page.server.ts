// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One guild's settings. Everything here is scoped to the id in the URL, and
// that id is never trusted: it has to appear in this broadcaster's own
// guilds.list or the route 404s, on the load AND on every action.
import type { Actions, PageServerLoad, RequestEvent } from './$types';
import {
  blankDiscordConfig,
  blankLayout,
  blankStatus,
  botStatus,
  guildLayout,
  listGuilds,
  persistSetup,
  pinnedRolesOf,
  readDiscord,
  readGuildConfig,
  repostDesk,
  saveGuildConfig,
  setupGuild,
  unbindGuild,
  type DiscordConfig,
  type DiscordGuildSummary,
  type DiscordGuildTarget,
  type DiscordLayout,
  type DiscordStatus
} from '$lib/server/discord-store';
import { discordConfigured, requireDiscordActor } from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleUnlocked, gateModulePage, moduleLocked } from '$lib/server/module-gate';
import { DISCORD_DEF } from '$lib/server/discord-def';
import { alertOff, alertOn, mergeDiscordConfig } from '@bagel/shared';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { error, fail, isRedirect, redirect } from '@sveltejs/kit';

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
const DEMO = dev && process.env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

type DiscordGuildPage = {
  locked: boolean;
  enabled: boolean;
  guildId: string;
  guilds: DiscordGuildSummary[];
  config: DiscordConfig;
  version: number;
  layout: DiscordLayout;
  status: DiscordStatus;
  configured: boolean;
  justConnected: boolean;
  refused: boolean;
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
    layout: blankLayout(),
    status: blankStatus(),
    configured: discordConfigured(),
    justConnected: false,
    refused: false,
    degraded: false
  };
}

/**
 * The ownership check, and the only one.
 *
 * guildId is a path segment, so it is caller-supplied: without this any
 * signed-in broadcaster could read (and save) another one's server just by
 * typing its id. `guilds.list` is derived from the bindings outgress owns, so
 * membership in it IS the proof. 404 rather than 403 on purpose: a 403 would
 * confirm the guild exists and is somebody's.
 */
function ownedGuild(guilds: DiscordGuildSummary[], guildId: string): DiscordGuildSummary {
  const found = guilds.find((g) => g.guildId === guildId);
  if (!found) throw error(404, 'No such server.');
  return found;
}

export const load: PageServerLoad = async ({ locals, params, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const guildId = params.guildId;
  const locked = await moduleLocked(locals, DISCORD_DEF);

  if (DEMO) return demoPage(guildId, url);

  const view = await readDiscord({ userId: uid }).catch(() => null);
  if (!view) return { ...blankPage(guildId, locked), degraded: true };
  ownedGuild(view.guilds, guildId);

  const target = { userId: uid, guildId };
  const flags = {
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
  return {
    ...blankPage(guildId, locked),
    ...flags,
    enabled: view.enabled,
    guilds: view.guilds,
    ...(await guildReads(target))
  };
};

async function demoPage(guildId: string, url: URL): Promise<DiscordGuildPage> {
  const { demoDiscordView, demoDiscordConfig, demoDiscordLayout, demoDiscordStatus } = await import(
    '$lib/server/demo-data'
  );
  const view = demoDiscordView();
  const g = ownedGuild(view.guilds, guildId);
  const row = demoDiscordConfig();
  // The layout and status fixtures describe one server; re-stamping them with
  // the picked guild is what makes switching servers in demo show a different
  // server rather than the same card twice under two names.
  const guild = { id: guildId, name: g.name, iconUrl: '', memberCount: g.memberCount };
  return {
    ...blankPage(guildId, false),
    enabled: view.enabled,
    guilds: view.guilds,
    config: { ...row.config, guildId },
    version: row.version,
    layout: { ...demoDiscordLayout(), guild, botOnline: g.botPresent },
    status: { ...demoDiscordStatus(), guild, online: g.botPresent, guildPresent: g.botPresent },
    configured: true,
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
}

// The config row is load-bearing (it is the draft the page edits), so a
// failure there degrades the page. Layout and status are decorative: the
// pickers fall back to a disabled control and the status card to an offline
// pill, so a blip in either must not take the page with it.
async function guildReads(
  target: DiscordGuildTarget
): Promise<{ config: DiscordConfig; version: number; layout: DiscordLayout; status: DiscordStatus; degraded: boolean }> {
  const [row, layout, status] = await Promise.all([
    readGuildConfig(target).catch((e) => {
      logger.warn({ err: e }, '[discord] guild config unavailable');
      return null;
    }),
    loadLayout(target),
    loadStatus(target)
  ]);
  if (!row) {
    return { config: { ...blankDiscordConfig(), guildId: target.guildId }, version: 0, layout, status, degraded: true };
  }
  return { config: row.config, version: row.version, layout, status, degraded: false };
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
  form: FormData;
};

async function actionContext({ request, locals, params }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  const uid = effectiveId(locals.session);
  const guildId = params.guildId;
  return { uid, guildId, target: { userId: uid, guildId }, session: locals.session, locals, form: await request.formData() };
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

// A soft refusal: the call reached outgress and it said no. `code` is what the
// page switches on; `error` is the human sentence it falls back to.
type Refusal = { error: string; code?: string };

function refusalOf(data: Record<string, unknown>): Refusal | null {
  const error = typeof data.error === 'string' ? data.error : '';
  const code = typeof data.code === 'string' ? data.code : '';
  if (!error && !code) return null;
  return { error, code };
}

// Ownership again, per action. A page that rendered before a guild was
// disconnected still posts to this URL, and the load's check does not run for
// a form POST.
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
    // The page is reachable while locked so it can explain itself; its writes
    // are not. Without this, a stale form on a downgraded board would still
    // save.
    if (!(await assertModuleUnlocked(event.locals, DISCORD_DEF))) {
      return fail(403, { ok: false, error: 'Discord is in beta and open to Premium channels only.' });
    }
    if (DEMO) return { ok: true };
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

/**
 * The whole draft arrives as one hidden JSON field.
 *
 * The old form posted a `name` per input plus a hidden mirror per switch,
 * which meant the tri-state flags had three sources of truth and a control the
 * page chose not to render silently cleared its field. One field means the
 * page's own state object IS the payload, and every rule lives in the shared
 * merge.
 */
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

/** The version the form was rendered against. A missing or unparseable one
 *  becomes 0, which outgress treats as "never read" and refuses on an existing
 *  row rather than clobbering it. */
function parseVersion(raw: FormDataEntryValue | null): number {
  if (typeof raw !== 'string') return 0;
  const n = Number.parseInt(raw.trim(), 10);
  return Number.isSafeInteger(n) && n >= 0 ? n : 0;
}

function fieldNames(errors: { field: string }[]): string {
  return errors.map((e) => e.field).join(', ');
}

export const actions: Actions = {
  save: discordAction({ label: 'save', failMsg: 'Could not save Discord settings.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    const { config, errors } = mergeDiscordConfig(row.config, parseDraft(ctx.form.get('config')));
    // A rejected field kept its stored value rather than being blanked, so the
    // save is still safe to apply; the refusal tells the streamer which
    // control did not take instead of leaving them to notice later.
    if (errors.length) return { error: `Some settings were not valid: ${fieldNames(errors)}.`, code: 'invalid' };
    const result = await saveGuildConfig({
      ...ctx.target,
      config,
      expectedVersion: parseVersion(ctx.form.get('version'))
    });
    if (result.error || result.code) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:save', ctx.guildId);
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
        // The saved toggle decides whether the fill creates the subscriber
        // tier, so setup reflects what the streamer chose rather than always
        // building a locked category they may never use.
        subscribers: alertOff(row.config.subscribersEnabled),
        // Pins win over name lookup, so a streamer who already has a Mods role
        // keeps it instead of getting a second one.
        pinnedRoles: pinnedRolesOf(row.config)
      },
      { ...row.config, twitchLogin: login }
    );
    if (result.error) return { error: result.error, code: result.code };
    const saved = await persistSetup(ctx.target, { ...result.config, twitchLogin: login });
    if (saved.error || saved.code) return { error: saved.error, code: saved.code };
    auditDashboardImpersonation(ctx.session, 'discord:setup', ctx.guildId);
    return { refused: result.refused };
  }),

  repost: discordAction({ label: 'repost', failMsg: 'Could not repost the ticket panel.' }, async (ctx) => {
    const row = await readGuildConfig(ctx.target);
    if (!alertOn(row.config.ticketsEnabled)) return { error: 'Turn the ticket desk on first.', code: 'invalid' };
    const result = await repostDesk(ctx.target);
    if (result.error) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:repost', ctx.guildId);
    return { messageId: result.messageId };
  }),

  // Scoped to this guild: the other servers this broadcaster owns keep their
  // bindings and their rows, and the master switch is not touched.
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
