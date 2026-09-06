<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild's sub-navigation. Real routes, not hash anchors.
  //
  // SectionNav is the in-page equivalent and it is deliberately NOT reused
  // here: it drives aria-current from window.location.hash and scrolls to a
  // section in the same document, which is exactly what the old single Discord
  // page did. Seven routes means seven URLs a streamer can bookmark, link a mod
  // to, and land on with only that page's data rendered -- and it means the
  // browser Back button walks the section instead of walking away from it. The
  // chip styling is copied rather than shared so a change to the in-page rail's
  // hash behaviour cannot silently change route navigation.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/shared';

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

<nav class="guild-nav" aria-label={t('discord.nav.label')}>
  {#each SEGMENTS as item (item.segment)}
    <a href="{root}{item.segment}" aria-current={isCurrent(item.segment) ? 'page' : undefined}>
      {t(item.key)}
    </a>
  {/each}
</nav>

<style>
  /* Chips when there is room, a stacked hairline rail when the host column is
     narrow. Container query rather than a viewport breakpoint, for the same
     reason SectionNav uses one: the shell's column width is what decides
     whether seven chips wrap into an unreadable block, not the device. */
  .guild-nav {
    container-type: inline-size;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    min-width: 0;
  }

  .guild-nav a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 8px 16px;
    border-radius: var(--bb-radius-pill, 100px);
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    text-decoration: none;
    white-space: nowrap;
    transition:
      color var(--bb-dur-fast, 160ms) ease,
      background var(--bb-dur-fast, 160ms) ease,
      border-color var(--bb-dur-fast, 160ms) ease;
  }
  .guild-nav a:hover {
    color: var(--bb-tan-pale, var(--bb-tan-light));
    background: rgba(201, 168, 124, 0.08);
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
  }
  .guild-nav a:focus-visible {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 3px;
  }
  .guild-nav a[aria-current='page'] {
    color: var(--bb-tan-pale, var(--bb-tan-light));
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    background: rgba(201, 168, 124, 0.1);
  }

  @container (max-width: 260px) {
    .guild-nav {
      flex-direction: column;
      flex-wrap: nowrap;
      gap: 2px;
      border-left: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    }
    .guild-nav a {
      display: flex;
      min-height: 0;
      padding: 8px 0 8px 14px;
      border: none;
      border-radius: 0;
      background: transparent;
      color: var(--bb-muted);
      position: relative;
    }
    .guild-nav a::before {
      content: '';
      position: absolute;
      left: -1px;
      top: 20%;
      bottom: 20%;
      width: 1px;
      background: var(--bb-tan);
      transform: scaleY(0);
      transition: transform var(--bb-dur-base, 240ms) var(--bb-ease-out-expo, cubic-bezier(0.19, 1, 0.22, 1));
    }
    .guild-nav a:hover {
      color: var(--bb-tan-light);
      background: transparent;
    }
    .guild-nav a[aria-current='page'] {
      color: var(--bb-tan-light);
      background: transparent;
      padding-left: 18px;
    }
    .guild-nav a[aria-current='page']::before {
      transform: scaleY(1);
    }
  }
</style>
