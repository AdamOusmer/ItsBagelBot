// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The server list. One broadcaster owns many guilds, so this route holds
// everything that is true of the account -- the master switch, the list, the
// invite path -- and /discord/[guildId] holds everything that is true of one
// server.
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

// process.env, not $env/dynamic/private: this route sits behind guard.ts on
// the boot import graph (see module-gate.ts).
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

  // Discord is premium-only while it is in beta. The route guard lets a
  // sectioned module through so the page can explain that rather than
  // bouncing the visitor to a grid Discord is no longer in; the page renders
  // a locked panel and every action refuses (see listAction).
  //
  // Resolved inside `read` rather than before moduleLoad so the delegate gate
  // still runs first (moduleLoad pins that order, and the tier lookup is an
  // RPC for a beta module); `blank` reads the value the read already settled,
  // which is why it is a captured let rather than a second lookup on the
  // degraded path.
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
  // The master switch is per broadcaster, not per guild: turning Discord off
  // stops Bagel posting in every server at once, which is what a streamer who
  // reaches for this control means.
  //
  // The beta lock is checked here rather than in the gate: it is a refusal
  // with its own status and copy (the page renders the upgrade panel), which
  // the skeleton's single "not signed in" refusal cannot say.
  toggle: moduleAction(
    'discord',
    'toggle',
    async (uid, f, locals) => {
      if (!(await assertModuleUnlocked(locals, DISCORD_DEF))) {
        return fail(403, { ok: false, error: 'Discord is in beta and open to Premium channels only.' });
      }
      const enabled = f.get('is_enabled') === 'on';
      const view = await readDiscord({ userId: uid });
      await saveDiscordModule({ userId: uid, enabled, twitchLogin: view.twitchLogin });
      return String(enabled);
    },
    { demo: DEMO, invalid: 'Could not toggle Discord.' }
  )
};
