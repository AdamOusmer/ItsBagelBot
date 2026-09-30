<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import SectionNav from '@bagel/ui/svelte/SectionNav.svelte';
  import { getI18n } from '@bagel/kit';

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

  function isCurrent(segment: string): boolean {
    const href = `${root}${segment}`;
    if (segment === '') return here === root || here === `${root}/`;
    return here === href || here.startsWith(`${href}/`);
  }

  const items = $derived(
    SEGMENTS.map((item) => ({
      href: `${root}${item.segment}`,
      label: t(item.key),
      current: isCurrent(item.segment)
    }))
  );
</script>

<SectionNav label={t('discord.nav.label')} {items} />
