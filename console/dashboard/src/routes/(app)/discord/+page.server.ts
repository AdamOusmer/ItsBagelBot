// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The server list. One broadcaster owns many guilds, so this route holds
// everything that is true of the account -- the master switch, the list, the
// invite path -- and /discord/[guildId] holds everything that is true of one
// server.
import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { readDiscord, saveDiscordModule, type DiscordGuildSummary } from '$lib/server/discord-store';
import { DISCORD_ERROR_SLUGS, discordConfigured, discordTemplateURL } from '$lib/server/discord-oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { assertModuleUnlocked, gateModulePage, moduleLocked } from '$lib/server/module-gate';
import { DISCORD_DEF } from '$lib/server/discord-def';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { fail } from '@sveltejs/kit';

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
const DEMO = dev && process.env.DEMO === '1';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'discord');
}

type DiscordListPage = {
  locked: boolean;
  enabled: boolean;
  guilds: DiscordGuildSummary[];
  templateURL: string;
  configured: boolean;
  errorSlug: string;
  degraded: boolean;
};

function blankPage(errorSlug: string, locked: boolean): DiscordListPage {
  return {
    locked,
    enabled: false,
    guilds: [],
    templateURL: discordTemplateURL(),
    configured: discordConfigured(),
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
  // a locked panel and every action refuses (see listAction).
  const locked = await moduleLocked(locals, DISCORD_DEF);

  if (DEMO) return demoPage();

  try {
    const view = await readDiscord({ userId: uid });
    return { ...blankPage(errorSlug, locked), ...view };
  } catch (e) {
    logger.warn({ err: e }, '[discord] server list unavailable');
    return { ...blankPage(errorSlug, locked), degraded: true };
  }
};

async function demoPage(): Promise<DiscordListPage> {
  const { demoDiscordView } = await import('$lib/server/demo-data');
  return { ...blankPage('', false), ...demoDiscordView(), templateURL: 'https://discord.new/demo', configured: true };
}

export const actions: Actions = {
  // The master switch is per broadcaster, not per guild: turning Discord off
  // stops Bagel posting in every server at once, which is what a streamer who
  // reaches for this control means.
  toggle: async (event: RequestEvent) => {
    gate(event.locals.session);
    if (!DEMO && !event.locals.session) return fail(401, { ok: false, error: 'Not signed in.' });
    if (!(await assertModuleUnlocked(event.locals, DISCORD_DEF))) {
      return fail(403, { ok: false, error: 'Discord is in beta and open to Premium channels only.' });
    }
    const form = await event.request.formData();
    const enabled = form.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled };
    const uid = effectiveId(event.locals.session);
    try {
      const view = await readDiscord({ userId: uid });
      await saveDiscordModule({ userId: uid, enabled, twitchLogin: view.twitchLogin });
    } catch (e) {
      logger.error({ err: e }, '[discord] toggle failed');
      return fail(400, { ok: false, error: 'Could not toggle Discord.' });
    }
    auditDashboardImpersonation(event.locals.session, 'discord:toggle', String(enabled));
    return { ok: true, enabled };
  }
};
