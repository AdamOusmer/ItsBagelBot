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
  type DiscordGuildTarget,
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
    const view = await readDiscord({ userId: uid });
    return {
      ...view,
      layout: view.connected ? await loadLayout({ userId: uid, guildId: view.config.guildId }) : NO_LAYOUT,
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
async function loadLayout(target: DiscordGuildTarget): Promise<DiscordLayout> {
  try {
    return await guildLayout(target);
  } catch (e) {
    logger.warn({ err: e }, '[discord] layout unavailable');
    return NO_LAYOUT;
  }
}

type ActionCtx = { uid: string; session: Session | null | undefined; locals: App.Locals; form: FormData };

async function actionContext({ request, locals }: RequestEvent): Promise<ActionCtx | null> {
  gate(locals.session);
  if (DEMO) {
    return { uid: effectiveId(locals.session), session: locals.session, locals, form: await request.formData() };
  }
  if (!locals.session) return null;
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

type Refusal = { error: string };

function refusalOf(data: Record<string, unknown>): string {
  if (!('error' in data)) return '';
  if (typeof data.error !== 'string') return '';
  return data.error;
}

function discordAction<T extends Record<string, unknown>>(
  work: ActionWork,
  run: (ctx: ActionCtx) => Promise<T | Refusal>
) {
  return async (event: RequestEvent) => {
    const ctx = await actionContext(event);
    if (!ctx) return fail(401, { ok: false, error: 'Not signed in.' });
    if (DEMO) return { ok: true, enabled: ctx.form.get('is_enabled') === 'on' };
    const r = await attempt(work, () => run(ctx));
    if (!r.ok) return fail(400, { ok: false, error: r.error });
    const refusal = refusalOf(r.data);
    if (refusal) return fail(400, { ok: false, error: refusal });
    return { ok: true, ...r.data };
  };
}

type FormField = { form: FormData; name: string; current: string };

function flag(field: FormField): string {
  return field.form.get(field.name) === 'on' ? 'on' : 'off';
}

const SNOWFLAKE = /^\d{17,20}$/;

function snowflake(field: FormField): string {
  const raw = field.form.get(field.name);
  if (raw === null) return field.current;
  const v = String(raw).trim();
  if (v === '') return v;
  if (SNOWFLAKE.test(v)) return v;
  return field.current;
}

function positiveInt(field: FormField): string {
  const raw = field.form.get(field.name);
  if (raw === null) return field.current;
  const n = Number.parseInt(String(raw), 10);
  if (!Number.isInteger(n)) return field.current;
  if (n < 1) return field.current;
  return String(n);
}

function mergeSettings(current: DiscordConfig, form: FormData): DiscordConfig {
  return {
    ...current,
    liveEnabled: flag({ form, name: 'liveEnabled', current: current.liveEnabled }),
    clipsEnabled: flag({ form, name: 'clipsEnabled', current: current.clipsEnabled }),
    raidEnabled: flag({ form, name: 'raidEnabled', current: current.raidEnabled }),
    giftEnabled: flag({ form, name: 'giftEnabled', current: current.giftEnabled }),
    cheerEnabled: flag({ form, name: 'cheerEnabled', current: current.cheerEnabled }),
    subMilestoneEnabled: flag({ form, name: 'subMilestoneEnabled', current: current.subMilestoneEnabled }),
    welcomeEnabled: flag({ form, name: 'welcomeEnabled', current: current.welcomeEnabled }),
    goodbyeEnabled: flag({ form, name: 'goodbyeEnabled', current: current.goodbyeEnabled }),
    voiceEnabled: flag({ form, name: 'voiceEnabled', current: current.voiceEnabled }),
    ticketsEnabled: flag({ form, name: 'ticketsEnabled', current: current.ticketsEnabled }),
    logsEnabled: flag({ form, name: 'logsEnabled', current: current.logsEnabled }),
    levelsEnabled: flag({ form, name: 'levelsEnabled', current: current.levelsEnabled }),
    giftMin: positiveInt({ form, name: 'giftMin', current: current.giftMin }),
    cheerMin: positiveInt({ form, name: 'cheerMin', current: current.cheerMin }),
    categoryAllow: String(form.get('categoryAllow') ?? current.categoryAllow),
    categoryDeny: String(form.get('categoryDeny') ?? current.categoryDeny),
    liveChannelId: snowflake({ form, name: 'liveChannelId', current: current.liveChannelId }),
    clipsChannelId: snowflake({ form, name: 'clipsChannelId', current: current.clipsChannelId }),
    alertsChannelId: snowflake({ form, name: 'alertsChannelId', current: current.alertsChannelId }),
    welcomeChannelId: snowflake({ form, name: 'welcomeChannelId', current: current.welcomeChannelId }),
    voiceHubId: snowflake({ form, name: 'voiceHubId', current: current.voiceHubId }),
    logChannelId: snowflake({ form, name: 'logChannelId', current: current.logChannelId }),
    ticketChannelId: snowflake({ form, name: 'ticketChannelId', current: current.ticketChannelId }),
    ticketCategoryId: snowflake({ form, name: 'ticketCategoryId', current: current.ticketCategoryId }),
    liveRoleId: snowflake({ form, name: 'liveRoleId', current: current.liveRoleId }),
    memberRoleId: snowflake({ form, name: 'memberRoleId', current: current.memberRoleId }),
    streamerDiscordId: snowflake({ form, name: 'streamerDiscordId', current: current.streamerDiscordId })
  };
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
    const config = mergeSettings(view.config, ctx.form);
    await saveDiscord({ userId: ctx.uid, enabled: view.enabled, config });
    auditDashboardImpersonation(ctx.session, 'discord:save', config.guildId);
    return {};
  }),

  setup: discordAction({ label: 'setup', failMsg: 'Could not set up this server.' }, async (ctx) => {
    requireDiscordActor(ctx.locals);
    const view = await readDiscord({ userId: ctx.uid });
    if (!view.config.guildId) return { error: 'Connect a server first.' };
    const login = ctx.session?.login ?? view.config.twitchLogin;
    const result = await setupGuild(
      { userId: ctx.uid, guildId: view.config.guildId },
      { ...view.config, twitchLogin: login }
    );
    if (result.error) return { error: result.error };
    await saveDiscord({ userId: ctx.uid, enabled: view.enabled, config: { ...result.config, twitchLogin: login } });
    auditDashboardImpersonation(ctx.session, 'discord:setup', view.config.guildId);
    return { refused: result.refused };
  }),

  disconnect: discordAction({ label: 'disconnect', failMsg: 'Could not disconnect Discord.' }, async (ctx) => {
    const view = await readDiscord({ userId: ctx.uid });
    if (view.config.guildId) await unbindGuild({ userId: ctx.uid, guildId: view.config.guildId });
    await saveDiscord({ userId: ctx.uid, enabled: false, config: blankDiscordConfig() });
    auditDashboardImpersonation(ctx.session, 'discord:disconnect', view.config.guildId);
    return {};
  })
};
