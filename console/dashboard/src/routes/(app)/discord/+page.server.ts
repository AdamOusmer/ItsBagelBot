// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import {
  blankDiscordConfig,
  blankLayout,
  blankStatus,
  botStatus,
  guildLayout,
  pinnedRolesOf,
  readDiscord,
  repostDesk,
  saveDiscord,
  setupGuild,
  unbindGuild,
  type DiscordConfig,
  type DiscordGuildTarget,
  type DiscordLayout,
  type DiscordStatus
} from '$lib/server/discord-store';
import {
  DISCORD_ERROR_SLUGS,
  discordConfigured,
  discordTemplateURL,
  requireDiscordActor
} from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleUnlocked, gateModulePage, moduleLocked } from '$lib/server/module-gate';
import { alertOff, alertOn, mergeDiscordConfig, type ModuleDef } from '@bagel/shared';
import { moduleDef } from '@bagel/shared';

// Resolved once. moduleDef returns undefined for an unknown id, and a silent
// undefined here would disable the beta gate rather than fail, so this throws
// at import time if the catalog ever drops the entry.
const DISCORD_DEF: ModuleDef = (() => {
  const def = moduleDef('discord');
  if (!def) throw new Error('discord module missing from MODULE_CATALOG');
  return def;
})();
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { fail, isRedirect } from '@sveltejs/kit';

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
const DEMO = dev && process.env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

type DiscordPage = {
  locked: boolean;
  enabled: boolean;
  connected: boolean;
  config: DiscordConfig;
  layout: DiscordLayout;
  status: DiscordStatus;
  templateURL: string;
  configured: boolean;
  justConnected: boolean;
  refused: boolean;
  errorSlug: string;
  degraded: boolean;
};

function blankPage(errorSlug: string): DiscordPage {
  return {
    locked: false,
    enabled: false,
    connected: false,
    config: blankDiscordConfig(),
    layout: blankLayout(),
    status: blankStatus(),
    templateURL: discordTemplateURL(),
    configured: discordConfigured(),
    justConnected: false,
    refused: false,
    errorSlug,
    degraded: false
  };
}

export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = (DISCORD_ERROR_SLUGS as readonly string[]).includes(rawSlug) ? rawSlug : '';

  // Discord is premium-only while it is in beta. The route guard lets a
  // sectioned module through so the page can explain that rather than
  // bouncing the visitor to a grid Discord is no longer in; the page renders
  // a locked panel and every action refuses (see discordAction).
  const locked = await moduleLocked(locals, DISCORD_DEF);

  if (DEMO) return demoPage();

  const flags = {
    justConnected: url.searchParams.get('connected') === '1',
    refused: url.searchParams.get('refused') === '1'
  };
  try {
    const view = await readDiscord({ userId: uid });
    const target = { userId: uid, guildId: view.config.guildId };
    return { ...blankPage(errorSlug), ...flags, ...view, locked, ...(await connectedReads(view.connected, target)) };
  } catch {
    return { ...blankPage(errorSlug), locked, degraded: true };
  }
};

async function demoPage(): Promise<DiscordPage> {
  const { demoDiscordView, demoDiscordLayout, demoDiscordStatus } = await import('$lib/server/demo-data');
  return {
    ...blankPage(''),
    ...demoDiscordView(),
    layout: demoDiscordLayout(),
    status: demoDiscordStatus(),
    templateURL: 'https://discord.new/demo',
    configured: true
  };
}

// Both reads are decorative: the pickers degrade to a disabled control and the
// status card to an offline pill, so a blip in either must not take the page
// with it (the same per-panel try/catch loyalty uses).
async function connectedReads(
  connected: boolean,
  target: DiscordGuildTarget
): Promise<{ layout: DiscordLayout; status: DiscordStatus }> {
  if (!connected) return { layout: blankLayout(), status: blankStatus() };
  const [layout, status] = await Promise.all([loadLayout(target), loadStatus(target)]);
  return { layout, status };
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

type ActionCtx = { uid: string; session: Session | null | undefined; locals: App.Locals; form: FormData };

async function actionContext({ request, locals }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session, locals, form: await request.formData() };
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
    if (DEMO) return { ok: true, enabled: ctx.form.get('is_enabled') === 'on' };
    const r = await attempt(work, () => run(ctx));
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

function fieldNames(errors: { field: string }[]): string {
  return errors.map((e) => e.field).join(', ');
}

export const actions: Actions = {
  toggle: discordAction({ label: 'toggle', failMsg: 'Could not toggle Discord.' }, async (ctx) => {
    const enabled = ctx.form.get('is_enabled') === 'on';
    const view = await readDiscord({ userId: ctx.uid });
    await saveDiscord({ userId: ctx.uid, enabled, config: view.config });
    auditDashboardImpersonation(ctx.session, 'discord:toggle', String(enabled));
    return { enabled };
  }),

  save: discordAction({ label: 'save', failMsg: 'Could not save Discord settings.' }, async (ctx) => {
    const view = await readDiscord({ userId: ctx.uid });
    const { config, errors } = mergeDiscordConfig(view.config, parseDraft(ctx.form.get('config')));
    // A rejected field kept its stored value rather than being blanked, so the
    // save is still safe to apply; the refusal tells the streamer which
    // control did not take instead of leaving them to notice later.
    if (errors.length) return { error: `Some settings were not valid: ${fieldNames(errors)}.`, code: 'invalid' };
    await saveDiscord({ userId: ctx.uid, enabled: view.enabled, config });
    auditDashboardImpersonation(ctx.session, 'discord:save', config.guildId);
    return {};
  }),

  setup: discordAction({ label: 'setup', failMsg: 'Could not set up this server.' }, async (ctx) => {
    requireDiscordActor(ctx.locals);
    const view = await readDiscord({ userId: ctx.uid });
    if (!view.config.guildId) return { error: 'Connect a server first.', code: 'not_bound' };
    const login = ctx.session?.login ?? view.config.twitchLogin;
    const result = await setupGuild(
      {
        userId: ctx.uid,
        guildId: view.config.guildId,
        // The saved toggle decides whether the fill creates the subscriber
        // tier, so setup reflects what the streamer chose rather than always
        // building a locked category they may never use.
        subscribers: alertOff(view.config.subscribersEnabled),
        // Pins win over name lookup, so a streamer who already has a Mods role
        // keeps it instead of getting a second one.
        pinnedRoles: pinnedRolesOf(view.config)
      },
      { ...view.config, twitchLogin: login }
    );
    if (result.error) return { error: result.error, code: result.code };
    await saveDiscord({ userId: ctx.uid, enabled: view.enabled, config: { ...result.config, twitchLogin: login } });
    auditDashboardImpersonation(ctx.session, 'discord:setup', view.config.guildId);
    return { refused: result.refused };
  }),

  repost: discordAction({ label: 'repost', failMsg: 'Could not repost the ticket panel.' }, async (ctx) => {
    const view = await readDiscord({ userId: ctx.uid });
    if (!view.config.guildId) return { error: 'Connect a server first.', code: 'not_bound' };
    if (!alertOn(view.config.ticketsEnabled)) return { error: 'Turn the ticket desk on first.', code: 'invalid' };
    const result = await repostDesk({ userId: ctx.uid, guildId: view.config.guildId });
    if (result.error) return { error: result.error, code: result.code };
    auditDashboardImpersonation(ctx.session, 'discord:repost', view.config.guildId);
    return { messageId: result.messageId };
  }),

  disconnect: discordAction({ label: 'disconnect', failMsg: 'Could not disconnect Discord.' }, async (ctx) => {
    const view = await readDiscord({ userId: ctx.uid });
    if (view.config.guildId) await unbindGuild({ userId: ctx.uid, guildId: view.config.guildId });
    await saveDiscord({ userId: ctx.uid, enabled: false, config: blankDiscordConfig() });
    auditDashboardImpersonation(ctx.session, 'discord:disconnect', view.config.guildId);
    return {};
  })
};
