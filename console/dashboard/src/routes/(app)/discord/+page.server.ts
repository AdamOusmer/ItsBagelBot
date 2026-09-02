// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import {
  blankDiscordConfig,
  readDiscord,
  saveDiscord,
  setupGuild,
  type DiscordConfig
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
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

const ERROR_SLUGS = ['oauth', 'unconfigured', 'setup', 'state'] as const;

export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = (ERROR_SLUGS as readonly string[]).includes(rawSlug) ? rawSlug : '';
  const justConnected = url.searchParams.get('connected') === '1';
  const refused = url.searchParams.get('refused') === '1';

  if (DEMO) {
    const { demoDiscordView } = await import('$lib/server/demo-data');
    return {
      ...demoDiscordView(),
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
      templateURL: discordTemplateURL(),
      configured: discordConfigured(),
      justConnected: false,
      refused: false,
      errorSlug,
      degraded: true
    };
  }
}

async function actionContext({ request, locals }: { request: Request; locals: App.Locals }) {
  gate(locals.session);
  if (!DEMO && !locals.session) return null;
  return { uid: effectiveId(locals.session), session: locals.session, form: await request.formData() };
}

const notSignedIn = () => fail(401, { ok: false, error: 'Not signed in.' });

function flag(form: FormData, name: string): string {
  return form.get(name) === 'on' ? 'on' : 'off';
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
    giftMin: String(form.get('giftMin') ?? current.giftMin).trim(),
    cheerMin: String(form.get('cheerMin') ?? current.cheerMin).trim(),
    categoryAllow: String(form.get('categoryAllow') ?? current.categoryAllow),
    categoryDeny: String(form.get('categoryDeny') ?? current.categoryDeny)
  };
}

export const actions: Actions = {
  toggle: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    const enabled = ctx.form.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled };
    try {
      const view = await readDiscord(ctx.uid);
      await saveDiscord(ctx.uid, enabled, view.config);
      auditDashboardImpersonation(ctx.session, 'discord:toggle', String(enabled));
      return { ok: true, enabled };
    } catch (e) {
      logger.error({ err: e }, '[discord] toggle failed');
      return fail(400, { ok: false, error: 'Could not toggle Discord.' });
    }
  },

  save: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    if (DEMO) return { ok: true };
    try {
      const view = await readDiscord(ctx.uid);
      const config = mergeSettings(view.config, ctx.form);
      await saveDiscord(ctx.uid, view.enabled, config);
      auditDashboardImpersonation(ctx.session, 'discord:save', config.guildId);
      return { ok: true };
    } catch (e) {
      logger.error({ err: e }, '[discord] save failed');
      return fail(400, { ok: false, error: 'Could not save Discord settings.' });
    }
  },

  setup: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    if (DEMO) return { ok: true };
    try {
      requireDiscordActor(event.locals);
      const view = await readDiscord(ctx.uid);
      if (!view.config.guildId) return fail(400, { ok: false, error: 'Connect a server first.' });
      const login = ctx.session?.login ?? view.config.twitchLogin;
      const result = await setupGuild(ctx.uid, view.config.guildId, {
        ...view.config,
        twitchLogin: login
      });
      if (result.error) return fail(400, { ok: false, error: result.error });
      await saveDiscord(ctx.uid, true, { ...result.config, twitchLogin: login });
      auditDashboardImpersonation(ctx.session, 'discord:setup', view.config.guildId);
      return { ok: true, refused: result.refused };
    } catch (e) {
      logger.error({ err: e }, '[discord] setup failed');
      return fail(400, { ok: false, error: 'Could not set up this server.' });
    }
  },

  disconnect: async (event) => {
    const ctx = await actionContext(event);
    if (!ctx) return notSignedIn();
    if (DEMO) return { ok: true };
    try {
      const view = await readDiscord(ctx.uid);
      await saveDiscord(ctx.uid, false, blankDiscordConfig());
      auditDashboardImpersonation(ctx.session, 'discord:disconnect', view.config.guildId);
      return { ok: true };
    } catch (e) {
      logger.error({ err: e }, '[discord] disconnect failed');
      return fail(400, { ok: false, error: 'Could not disconnect Discord.' });
    }
  }
};
