// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import {
  blankDiscordConfig,
  guildLayout,
  readDiscord,
  saveDiscord,
  setupGuild,
  unbindGuild,
  type DiscordConfig,
  type DiscordLayout
} from '$lib/server/discord-store';
import {
  discordConfigured,
  discordTemplateURL,
  requireDiscordActor
} from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
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

const ERROR_SLUGS = ['oauth', 'unconfigured', 'setup', 'state', 'bound'] as const;

const NO_LAYOUT: DiscordLayout = { channels: [], roles: [] };

export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = (ERROR_SLUGS as readonly string[]).includes(rawSlug) ? rawSlug : '';
  const justConnected = url.searchParams.get('connected') === '1';
  const refused = url.searchParams.get('refused') === '1';

  if (DEMO) {
    const { demoDiscordView, demoDiscordLayout } = await import('$lib/server/demo-data');
    return {
      ...demoDiscordView(),
      layout: demoDiscordLayout(),
      templateURL: 'https://discord.new/demo',
      configured: true,
      justConnected: false,
      refused: false,
      errorSlug: ''
    };
  }

  try {
    const view = await readDiscord(uid);
    return {
      ...view,
      layout: view.connected ? await loadLayout(uid, view.config.guildId) : NO_LAYOUT,
      templateURL: discordTemplateURL(),
      configured: discordConfigured(),
      justConnected,
      refused,
      errorSlug
    };
  } catch {
    return {
      enabled: false,
      connected: false,
      config: blankDiscordConfig(),
      layout: NO_LAYOUT,
      templateURL: discordTemplateURL(),
      configured: discordConfigured(),
      justConnected: false,
      refused: false,
      errorSlug,
      degraded: true
    };
  }
};

// loadLayout is best-effort: without it the page falls back to raw id inputs.
async function loadLayout(uid: string, guildId: string): Promise<DiscordLayout> {
  try {
    return await guildLayout(uid, guildId);
  } catch (e) {
    logger.warn({ err: e }, '[discord] layout unavailable');
    return NO_LAYOUT;
  }
}

type ActionCtx = { uid: string; session: Session | null | undefined; locals: App.Locals; form: FormData };

async function actionContext({ request, locals }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session, locals, form: await request.formData() };
}

type Outcome<T> = { ok: true; data: T } | { ok: false; error: string };

// attempt wraps one backend round trip: a thrown redirect propagates, any
// other failure is logged once and mapped onto the action's user-facing line.
async function attempt<T>(label: string, failMsg: string, work: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, data: await work() };
  } catch (e) {
    if (isRedirect(e)) throw e;
    logger.error({ err: e }, `[discord] ${label} failed`);
    return { ok: false, error: failMsg };
  }
}

type Refusal = { error: string };

// discordAction is the shape every action here shares: gate, sign-in check,
// demo short-circuit, one backend round trip, one user-facing failure line.
// work may return a Refusal to fail with a specific message.
function discordAction<T extends Record<string, unknown>>(
  label: string,
  failMsg: string,
  work: (ctx: ActionCtx) => Promise<T | Refusal>
) {
  return async (event: RequestEvent) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: 'Not signed in.' });
    if (DEMO) return { ok: true, enabled: ctx.form.get('is_enabled') === 'on' };
    const r = await attempt(label, failMsg, () => work(ctx));
    if (!r.ok) return fail(400, { ok: false, error: r.error });
    if ('error' in r.data && typeof r.data.error === 'string' && r.data.error) {
      return fail(400, { ok: false, error: r.data.error });
    }
    return { ok: true, ...r.data };
  };
}

function flag(form: FormData, name: string): string {
  return form.get(name) === 'on' ? 'on' : 'off';
}

const SNOWFLAKE = /^\d{17,20}$/;

// snowflake accepts a Discord id or empty; anything else keeps the current
// value so a tampered select cannot store junk the workers would post into.
function snowflake(form: FormData, name: string, current: string): string {
  const raw = form.get(name);
  if (raw === null) return current;
  const v = String(raw).trim();
  return v === '' || SNOWFLAKE.test(v) ? v : current;
}

// positiveInt keeps the current threshold unless the form carries an integer
// of at least one; type="number" is only a client-side hint.
function positiveInt(form: FormData, name: string, current: string): string {
  const raw = form.get(name);
  if (raw === null) return current;
  const n = Number.parseInt(String(raw), 10);
  return Number.isInteger(n) && n >= 1 ? String(n) : current;
}

function mergeSettings(current: DiscordConfig, form: FormData): DiscordConfig {
  return {
    ...current,
    liveEnabled: flag(form, 'liveEnabled'),
    clipsEnabled: flag(form, 'clipsEnabled'),
    raidEnabled: flag(form, 'raidEnabled'),
    giftEnabled: flag(form, 'giftEnabled'),
    cheerEnabled: flag(form, 'cheerEnabled'),
    subMilestoneEnabled: flag(form, 'subMilestoneEnabled'),
    welcomeEnabled: flag(form, 'welcomeEnabled'),
    goodbyeEnabled: flag(form, 'goodbyeEnabled'),
    voiceEnabled: flag(form, 'voiceEnabled'),
    giftMin: positiveInt(form, 'giftMin', current.giftMin),
    cheerMin: positiveInt(form, 'cheerMin', current.cheerMin),
    categoryAllow: String(form.get('categoryAllow') ?? current.categoryAllow),
    categoryDeny: String(form.get('categoryDeny') ?? current.categoryDeny),
    liveChannelId: snowflake(form, 'liveChannelId', current.liveChannelId),
    clipsChannelId: snowflake(form, 'clipsChannelId', current.clipsChannelId),
    alertsChannelId: snowflake(form, 'alertsChannelId', current.alertsChannelId),
    welcomeChannelId: snowflake(form, 'welcomeChannelId', current.welcomeChannelId),
    voiceHubId: snowflake(form, 'voiceHubId', current.voiceHubId),
    liveRoleId: snowflake(form, 'liveRoleId', current.liveRoleId),
    streamerDiscordId: snowflake(form, 'streamerDiscordId', current.streamerDiscordId)
  };
}

export const actions: Actions = {
  toggle: discordAction('toggle', 'Could not toggle Discord.', async (ctx) => {
    const enabled = ctx.form.get('is_enabled') === 'on';
    const view = await readDiscord(ctx.uid);
    await saveDiscord(ctx.uid, enabled, view.config);
    auditDashboardImpersonation(ctx.session, 'discord:toggle', String(enabled));
    return { enabled };
  }),

  save: discordAction('save', 'Could not save Discord settings.', async (ctx) => {
    const view = await readDiscord(ctx.uid);
    const config = mergeSettings(view.config, ctx.form);
    await saveDiscord(ctx.uid, view.enabled, config);
    auditDashboardImpersonation(ctx.session, 'discord:save', config.guildId);
    return {};
  }),

  setup: discordAction('setup', 'Could not set up this server.', async (ctx) => {
    requireDiscordActor(ctx.locals);
    const view = await readDiscord(ctx.uid);
    if (!view.config.guildId) return { error: 'Connect a server first.' };
    const login = ctx.session?.login ?? view.config.twitchLogin;
    const result = await setupGuild(ctx.uid, view.config.guildId, { ...view.config, twitchLogin: login });
    if (result.error) return { error: result.error };
    await saveDiscord(ctx.uid, view.enabled, { ...result.config, twitchLogin: login });
    auditDashboardImpersonation(ctx.session, 'discord:setup', view.config.guildId);
    return { refused: result.refused };
  }),

  disconnect: discordAction('disconnect', 'Could not disconnect Discord.', async (ctx) => {
    const view = await readDiscord(ctx.uid);
    if (view.config.guildId) await unbindGuild(ctx.uid, view.config.guildId);
    await saveDiscord(ctx.uid, false, blankDiscordConfig());
    auditDashboardImpersonation(ctx.session, 'discord:disconnect', view.config.guildId);
    return {};
  })
};
