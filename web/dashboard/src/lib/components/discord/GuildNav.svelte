<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild's sub-navigation, on THE RAIL (`.bb-tabs`, @bagel/ui/styles/tags.css).
  //
  // The SectionNav ADAPTER is deliberately not reused: it drives the active
  // item from window.location.hash and scrolls to a section in the same
  // document, which is what the old single Discord page did. Seven routes
  // means seven URLs a streamer can bookmark, link a mod to, and land on with
  // only that page's data rendered -- and it means the browser Back button
  // walks the section instead of walking away from it.
  //
  // The CONTRACT is shared, and that half used to be copied: this file carried
  // its own 70-line pill-then-hairline block, hand-ported from SectionNav's,
  // so the console had a third answer to "which of these am I looking at" that
  // drifted from the other two (pills at rest, a 260px container breakpoint
  // against the rail's 220px, a tan active marker against the rail's green).
  // Rendering `.bb-tabs` directly keeps the route behaviour local and the look
  // shared, which is the split that was wanted; only `aria-current` is ours,
  // because a route rail marks the current PAGE and an in-page rail does not.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit';
  import '@bagel/ui/styles/tags.css';

  let { guildId }: { guildId: string } = $props();
  const { t } = getI18n();

  type NavKey =
    | 'discord.nav.overview'
    | 'discord.nav.channels'
    | 'discord.nav.roles'
    | 'discord.nav.announcements'
    | 'discord.nav.community'
    | 'discord.nav.tickets'
    | 'discord.nav.settings';

  // Segment '' is the overview, which is the guild root itself.
  const SEGMENTS: { segment: string; key: NavKey }[] = [
    { segment: '', key: 'discord.nav.overview' },
    { segment: '/channels', key: 'discord.nav.channels' },
    { segment: '/roles', key: 'discord.nav.roles' },
    { segment: '/announcements', key: 'discord.nav.announcements' },
    { segment: '/community', key: 'discord.nav.community' },
    { segment: '/tickets', key: 'discord.nav.tickets' },
    { segment: '/settings', key: 'discord.nav.settings' }
  ];

  const root = $derived(`/discord/${guildId}`);
  const here = $derived(page.url.pathname);

  // Exact match for the overview, prefix for the rest: the overview's href is a
  // prefix of every other one, so a prefix test would light it up on every
  // page. A trailing slash is tolerated because SvelteKit's trailingSlash
  // setting is a deploy-time choice this component should not depend on.
  function isCurrent(segment: string): boolean {
    const href = `${root}${segment}`;
    if (segment === '') return here === root || here === `${root}/`;
    return here === href || here.startsWith(`${href}/`);
  }
</script>

<div class="bb-tabs-host">
  <nav class="bb-tabs bb-tabs--auto" aria-label={t('discord.nav.label')}>
    {#each SEGMENTS as item (item.segment)}
      {@const on = isCurrent(item.segment)}
      <a
        class="bb-tab"
        class:is-active={on}
        href="{root}{item.segment}"
        aria-current={on ? 'page' : undefined}
      >
        {t(item.key)}
      </a>
    {/each}
  </nav>
</div>
