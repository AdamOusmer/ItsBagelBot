<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The rail's account foot. When the rail is on screen this is the account
  // surface: the row opens the switcher menu (shared dashboards + delegate
  // exit) that used to live behind the topbar avatar, so the signed-in user
  // has one place to be, not two.
  import { afterNavigate } from '$app/navigation';
  import Button from './Button.svelte';
  import Bolota from './Bolota.svelte';
  import Icon from './Icon.svelte';
  import Scroller from './Scroller.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  // Falls back to English when no i18n context is set (admin).
  const { t } = getI18n();

  let {
    name,
    role,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    name: string;
    role: string;
    // Boards shared with this user; renders a scrollable quick-switch list in
    // the account menu. Empty (e.g. a user with no grants) hides it.
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  // Wakes the Bolota engine while the pointer is over the account row.
  let hovered = $state(false);

  // Nothing to switch to and nowhere to exit back to: the row stays a plain
  // readout instead of a button that opens an empty menu.
  const hasMenu = $derived(dashboards.length > 0 || isDelegate);
  let menuOpen = $state(false);

  // The rail lives in the persistent layout, so a shared-dashboard link in the
  // menu navigates without unmounting it, leaving the menu open. Close it on
  // any completed navigation (covers back/forward too).
  afterNavigate(() => (menuOpen = false));
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<div class="side-foot">
  {#if hasMenu}
    <button
      class="account account--btn"
      class:open={menuOpen}
      type="button"
      aria-expanded={menuOpen}
      aria-haspopup="menu"
      onclick={() => (menuOpen = !menuOpen)}
      onpointerenter={() => (hovered = true)}
      onpointerleave={() => (hovered = false)}
    >
      <span class="avatar"><Bolota name={name} size={34} active={hovered || menuOpen} /></span>
      <span class="who">
        <b>{name}</b>
        <span>{role}</span>
      </span>
      <span class="chev" class:open={menuOpen} aria-hidden="true">
        <svg viewBox="0 0 24 24" width="14" height="14"><polyline points="9 6 15 12 9 18"></polyline></svg>
      </span>
    </button>
    {#if menuOpen}
      <!-- Click-away scrim; Escape via the window handler above. -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="foot-scrim"
        role="presentation"
        onclick={() => (menuOpen = false)}
        onkeydown={(e) => { if (e.key === 'Enter') menuOpen = false; }}
      ></div>
      <div class="foot-menu" role="menu">
        <!-- Centrepiece, carried over from the topbar menu this replaced: a big
             Bolota on its own plate, name and role stacked under it. -->
        <div class="foot-menu-head">
          <span class="foot-menu-avatar">
            <Bolota name={name} size={72} active={menuOpen} />
          </span>
          <b>{name}</b>
          <i>{role}</i>
        </div>
        {#if dashboards.length}
          <!-- The Scroller caps the list so a long roster never runs the menu
               off the top of the rail. Each row jumps into that owner's
               dashboard via the /delegate/enter link. -->
          <div class="foot-menu-section">{t('topbar.dashboards')}</div>
          <Scroller maxHeight="208px" role="group" aria-label={t('topbar.dashboards')}>
            <div class="foot-dash-list">
              {#each dashboards as d (d.href)}
                <a class="foot-dash" href={d.href} role="menuitem">
                  <span class="dash-avatar"><Bolota name={d.name} size={26} active={menuOpen} gate /></span>
                  <span class="dash-name">{d.name}</span>
                </a>
              {/each}
            </div>
          </Scroller>
        {/if}
        {#if isDelegate}
          <a class="foot-dash" href={delegateExitHref} role="menuitem">
            <span class="dash-avatar"><Icon name="home" size={14} /></span>
            <span class="dash-name">{delegateExitLabel}</span>
          </a>
        {/if}
      </div>
    {/if}
  {:else}
    <div class="account" role="group" onpointerenter={() => (hovered = true)} onpointerleave={() => (hovered = false)}>
      <div class="avatar"><Bolota name={name} size={34} active={hovered} /></div>
      <div class="who">
        <b>{name}</b>
        <span>{role}</span>
      </div>
    </div>
  {/if}
  <form method="POST" action="/auth/logout" onsubmit={() => localStorage.removeItem('bb-onboarded')}>
    <Button variant="ghost" type="submit" icon="power" style="width:100%;justify-content:center;margin-top:10px">
      {t('topbar.logout')}
    </Button>
  </form>
</div>

<style>
  /* Anchor for the pop-up menu. */
  .side-foot { position: relative; border-top: 1px solid var(--glass-border); padding-top: 14px; margin-top: 10px; }
  .account { display: flex; align-items: center; gap: 10px; padding: 6px 8px; }
  .account--btn {
    width: 100%; text-align: left;
    background: none; border: none; border-radius: 10px; cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .account--btn:hover, .account--btn.open { background: rgba(201, 168, 124, 0.1); }
  .avatar { width: 34px; height: 34px; border-radius: 50%; flex-shrink: 0;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan)); position: relative;
    display: flex; align-items: center; justify-content: center; }
  .who { line-height: 1.2; min-width: 0; flex: 1; display: block; }
  .who b { font-size: 13px; font-weight: 600; color: var(--bb-white); display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .who span { font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; color: var(--bb-tan); }

  .chev { flex: none; display: flex; color: var(--bb-muted); }
  /* Points up when closed (the menu opens upward), down when open. The
     rotation sits on the svg because that's where the transition lives. */
  .chev svg { fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; transform: rotate(-90deg); transition: transform var(--bb-dur-base) var(--bb-ease-out-expo); }
  .chev.open svg { transform: rotate(90deg); }

  .foot-scrim { position: fixed; inset: 0; z-index: 89; }
  /* Opens upward from the foot, spanning the rail's inner width. It stays
     inside the rail's own stacking/scroll box, which is fine: the rail is
     100vh, so the menu overlays the nav rows above the foot. */
  .foot-menu {
    position: absolute;
    bottom: calc(100% + 8px);
    left: 0; right: 0;
    z-index: 90;
    padding: 8px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: 8px 8px;
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    transform-origin: bottom left;
    animation: foot-menu-in 240ms var(--bb-ease-out-back, ease-out) both;
  }
  @keyframes foot-menu-in {
    from { opacity: 0; transform: translateY(6px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  .foot-menu-head {
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    padding: 14px 10px 16px; border-bottom: 1px solid var(--bb-border); margin-bottom: 6px;
    text-align: center;
  }
  .foot-menu-avatar {
    width: 84px; height: 84px; border-radius: 50%; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
  }
  .foot-menu-head b {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 14px; color: var(--bb-white);
    max-width: 100%; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .foot-menu-head i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 10px; color: var(--bb-tan); }

  .foot-menu-section {
    padding: 2px 10px 4px;
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--bb-tan);
  }
  .foot-dash-list { display: flex; flex-direction: column; gap: 2px; }
  .foot-dash {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 7px 10px; border-radius: 8px 8px;
    text-decoration: none; cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .foot-dash:hover { background: rgba(201, 168, 124, 0.1); }
  /* Same gradient plate as the account badge, scaled down. */
  .dash-avatar {
    width: 26px; height: 26px; border-radius: 50%; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
  }
  .dash-name {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-muted);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
  }
  .foot-dash:hover .dash-name { color: var(--bb-white); }

  @media (prefers-reduced-motion: reduce) {
    .foot-menu { animation: none; }
    .chev svg { transition: none; }
  }
</style>
