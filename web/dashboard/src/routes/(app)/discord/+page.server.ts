// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { readDiscord, saveDiscordModule, type DiscordGuildSummary } from '$lib/server/discord-store';
import { DISCORD_ERROR_SLUGS, discordConfigured, discordTemplateURL } from '$lib/server/discord-oauth';
import { logger } from '@bagel/kit/server/logger';
import { assertModuleUnlocked, moduleLocked } from '$lib/server/module-gate';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction } from '$lib/server/module-action';
import { DISCORD_DEF } from '$lib/server/discord-def';
import { dev } from '$app/environment';
import { fail } from '@sveltejs/kit';
import { actionError } from '$lib/server/action-errors';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

type DiscordListPage = {
  locked: boolean;
  enabled: boolean;
  guilds: DiscordGuildSummary[];
  truncated: boolean;
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
    truncated: false,
    templateURL: discordTemplateURL(),
    configured: discordConfigured(),
    errorSlug,
    degraded: false
  };
}

export const load: PageServerLoad = ({ locals, url }) => {
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = (DISCORD_ERROR_SLUGS as readonly string[]).includes(rawSlug) ? rawSlug : '';

  let locked = false;
  return moduleLoad<DiscordListPage>('discord', locals.session, {
    demo: DEMO ? demoPage : undefined,
    read: async (uid) => {
      locked = await moduleLocked(locals, DISCORD_DEF);
      const view = await readDiscord({ userId: uid }).catch((e) => {
        logger.warn({ err: e }, '[discord] server list unavailable');
        throw e;
      });
      return { ...blankPage(errorSlug, locked), ...view };
    },
    blank: () => blankPage(errorSlug, locked)
  });
};

async function demoPage(): Promise<DiscordListPage> {
  const { demoDiscordView } = await import('$lib/server/demo-data');
  return { ...blankPage('', false), ...demoDiscordView(), templateURL: 'https://discord.new/demo', configured: true };
}

export const actions: Actions = {
  toggle: moduleAction(
    'discord',
    'toggle',
    async (uid, f, locals) => {
      if (!(await assertModuleUnlocked(locals, DISCORD_DEF))) {
        return fail(403, { ok: false, error: actionError(locals.locale, 'Discord is in beta and open to Premium channels only.') });
      }
      const enabled = f.get('is_enabled') === 'on';
      const view = await readDiscord({ userId: uid });
      await saveDiscordModule({ userId: uid, enabled, twitchLogin: view.twitchLogin });
      return String(enabled);
    },
    { demo: DEMO, invalid: 'Could not toggle Discord.' }
  )
};
