<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Rail from '@bagel/ui/svelte/Rail.svelte';
  import { Browser } from '@wailsio/runtime';
  import { onMount } from 'svelte';
  import { signOut } from '../lib/auth.svelte';
  import { NAV_ITEMS, RAIL_COLLAPSED_KEY, type SectionId } from '../lib/nav';
  import { SUPPORT_LINKS, BUG_REPORT_LINK } from '../lib/support-links';
  import Connections from './shell/Connections.svelte';
  import OverlayEditor from './shell/OverlayEditor.svelte';
  import Overview from './shell/Overview.svelte';
  import Recipes from './shell/Recipes.svelte';
  import Settings from './shell/Settings.svelte';

  // Sample account until device pairing exists; see lib/auth.svelte.ts.
  const SAMPLE_DASHBOARDS = [{ href: 'https://dashboard.itsbagelbot.com/', name: 'sample_channel' }];
  const BRAND = { title: 'ItsBagelBot', sub: 'Console', logoSrc: '/logo.png', logoAlt: '' };

  const ACCOUNT = {
    name: 'Mavey',
    caption: 'Broadcaster',
    links: SAMPLE_DASHBOARDS.map((d) => ({ href: d.href, label: d.name })),
    linksLabel: 'Shared dashboards',
    help: SUPPORT_LINKS,
    helpLabel: 'Support',
    feedback: BUG_REPORT_LINK,
    logoutLabel: 'Log out',
    onLogout(event: SubmitEvent): void {
      event.preventDefault();
      signOut();
    },
  };

  // macOS window controls span x 18 to 80 and y 18 to 34, so they get their own
  // row and the collapsed rail stays wide enough to hold all three.
  const isMac = navigator.userAgent.includes('Mac');
  const RAIL_TOP_INSET = isMac ? 52 : undefined;
  const RAIL_COLLAPSED_WIDTH = isMac ? 98 : undefined;

  function readCollapsed(): boolean {
    try {
      return localStorage.getItem(RAIL_COLLAPSED_KEY) === '1';
    } catch {
      return false;
    }
  }

  const DEV_SECTION = import.meta.env.DEV ? (import.meta.env.VITE_SECTION as SectionId | undefined) : undefined;

  let activeSection = $state<SectionId>(DEV_SECTION ?? 'overview');
  let collapsed = $state(readCollapsed());
  let overlayCollapsed = $state(true);

  const groups = $derived([
    {
      label: 'Manage',
      items: NAV_ITEMS.map((item) => ({
        href: `#${item.id}`,
        icon: item.icon,
        label: item.label,
        current: activeSection === item.id,
      })),
    },
  ]);

  $effect(() => {
    try {
      localStorage.setItem(RAIL_COLLAPSED_KEY, collapsed ? '1' : '0');
    } catch {
      // Storage may be unavailable (private mode, quota); the toggle still works for this session.
    }
  });

  function navigate(href: string): void {
    activeSection = href.slice(1) as SectionId;
  }

  function railCollapsed(): boolean {
    return activeSection === 'overlay' ? overlayCollapsed : collapsed;
  }

  function setRailCollapsed(next: boolean): void {
    if (activeSection === 'overlay') overlayCollapsed = next;
    else collapsed = next;
  }

  // A webview ignores target=_blank, so web and mail links have to leave through the OS.
  function openExternally(event: MouseEvent): void {
    const link = (event.target as Element).closest<HTMLAnchorElement>('a[href^="http"], a[href^="mailto:"]');
    if (!link) return;
    event.preventDefault();
    Browser.OpenURL(link.href).catch(() => window.open(link.href, '_blank', 'noopener'));
  }

  onMount(() => {
    document.addEventListener('click', openExternally);
    return () => document.removeEventListener('click', openExternally);
  });
</script>

<div class="shell">
  <div class="shell__drag" aria-hidden="true"></div>

  <div class="shell__body">
    <Rail
      brand={BRAND}
      {groups}
      account={ACCOUNT}
      collapsible
      alwaysVisible
      bind:collapsed={railCollapsed, setRailCollapsed}
      topInset={RAIL_TOP_INSET}
      collapsedWidth={RAIL_COLLAPSED_WIDTH}
      onnavigate={navigate}
    />

    <main class="shell__main">
      {#if activeSection === 'overview'}
        <Overview />
      {:else if activeSection === 'recipes'}
        <Recipes />
      {:else if activeSection === 'overlay'}
        <OverlayEditor />
      {:else if activeSection === 'connections'}
        <Connections />
      {:else if activeSection === 'settings'}
        <Settings />
      {/if}
    </main>
  </div>
</div>

<style>
  .shell {
    position: relative;
    isolation: isolate;
    height: 100svh;
    overflow: hidden;
  }

  .shell__drag {
    --wails-draggable: drag;
    position: absolute;
    top: 8px;
    left: 80px;
    right: 24px;
    z-index: 1;
    height: 44px;
  }

  .shell__body {
    display: flex;
    height: 100%;
    min-height: 0;
  }

  .shell__main {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: auto;
    padding: 28px 32px;
  }

  .shell__main:has(:global([data-canvas='fill'])) {
    display: flex;
    overflow: hidden;
    padding: 0;
  }
</style>
