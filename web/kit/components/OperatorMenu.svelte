<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The signed-in operator: an avatar chip that opens a menu with the boards
  // shared with this account and a way out.
  //
  // Lifted out of Topbar.svelte when the strip became @bagel/ui's
  // `.bb-topbar`. It could not go with it, and the reasons are the plan's own
  // rule for what stays in kit: it reads `$app/navigation`, it renders a
  // `POST /auth/logout` form against a route only this app has, it draws an
  // avatar with a third-party engine (@luzir/bolota), and every string in it
  // comes from the console's i18n catalog. A design library that knew any one
  // of those could not be extracted.
  //
  // What it renders INTO is ui's `account` slot, so the strip does not know
  // this exists and this does not know how the strip is laid out.
  import { afterNavigate } from '$app/navigation';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  // Falls back to English when no i18n context is set (admin), so the labels
  // are correct in every app without prop-drilling them through the shell.
  const { t } = getI18n();

  let {
    accountName,
    accountRole,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    accountName: string;
    accountRole: string;
    // Boards shared with this user; renders a scrollable quick-switch list.
    // Empty (admin, or a user with no grants) hides the section.
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let menuOpen = $state(false);

  // Drives the chip's Bolota: it wakes up on hover, independently of whether
  // the menu is open.
  let hovered = $state(false);

  // This lives in the persistent layout, so a shared-board link navigates
  // without unmounting it and would leave the menu hanging open. Close on any
  // completed navigation (which covers back/forward too).
  afterNavigate(() => (menuOpen = false));
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<button
  class="operator"
  class:open={menuOpen}
  title="{accountName} · {accountRole}"
  aria-label="{accountName} · {accountRole}"
  aria-expanded={menuOpen}
  aria-haspopup="menu"
  onclick={() => (menuOpen = !menuOpen)}
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  <span class="avatar"><Bolota name={accountName} size={30} active={hovered || menuOpen} /></span>
  <span class="op-id">
    <b>{accountName}</b>
    <i>{accountRole}</i>
  </span>
</button>
{#if menuOpen}
  <!-- Click-away scrim; Escape via the window handler above. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="op-scrim"
    role="presentation"
    onclick={() => (menuOpen = false)}
    onkeydown={(e) => { if (e.key === 'Enter') menuOpen = false; }}
  ></div>
  <div class="op-menu" role="menu">
    <div class="op-menu-head">
      <span class="op-menu-avatar">
        <Bolota name={accountName} size={72} active={menuOpen} />
      </span>
      <b>{accountName}</b>
      <i>{accountRole}</i>
    </div>
    {#if dashboards.length}
      <!-- Boards shared with this user; the Scroller caps the list so a long
           roster never runs the menu off-screen. Each row jumps into that
           owner's dashboard via the /delegate/enter link. -->
      <div class="op-dash-group">
        <div class="op-menu-section">{t('topbar.dashboards')}</div>
        <Scroller maxHeight="208px" role="group" aria-label={t('topbar.dashboards')}>
          <div class="op-dash-list">
            {#each dashboards as d (d.href)}
              <a class="op-dash" href={d.href} role="menuitem">
                <span class="dash-avatar"><Bolota name={d.name} size={26} active={menuOpen} gate /></span>
                <span class="dash-name">{d.name}</span>
              </a>
            {/each}
          </div>
        </Scroller>
      </div>
    {/if}
    {#if isDelegate}
      <div class="op-dash-group">
        <a class="op-dash" href={delegateExitHref} role="menuitem">
          <span class="dash-avatar"><Icon name="home" size={14} /></span>
          <span class="dash-name">{delegateExitLabel}</span>
        </a>
      </div>
    {/if}
    <form method="POST" action="/auth/logout">
      <button type="submit" class="op-menu-item" role="menuitem">
        {t('topbar.logout')}
      </button>
    </form>
  </div>
{/if}

<style>
  .operator {
    display: flex; align-items: center; gap: 9px;
    background: none; border: none; padding: 3px; border-radius: var(--bb-radius-pill);
    cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .operator:hover, .operator.open { background: rgba(201, 168, 124, 0.1); }
  .avatar {
    width: 30px; height: 30px; border-radius: 50%; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
  }
  .op-id { display: none; flex-direction: column; line-height: 1; text-align: left; }
  .op-id b { font-family: var(--bb-font-body); font-weight: 600; font-size: 12px; color: var(--bb-white); max-width: 140px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .op-id i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 10px; letter-spacing: 0.02em; color: var(--bb-tan); margin-top: 3px; }

  .op-scrim { position: fixed; inset: 0; z-index: 89; }
  .op-menu {
    position: absolute;
    top: calc(100% + 10px);
    right: 0;
    z-index: 90;
    min-width: 260px;
    padding: 8px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: var(--bb-radius-md);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    transform-origin: top right;
    animation: menu-in 240ms var(--bb-ease-out-back, ease-out) both;
  }
  @keyframes menu-in {
    from { opacity: 0; transform: translateY(-6px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }
  /* Centrepiece of the menu: a big Bolota on its own plate, name and role
     stacked below it, everything centred. */
  .op-menu-head {
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    padding: 14px 10px 16px; border-bottom: 1px solid var(--bb-border); margin-bottom: 6px;
    text-align: center;
  }
  .op-menu-avatar {
    width: 84px; height: 84px; border-radius: 50%; flex: none;
    background: linear-gradient(135deg, var(--bb-green-light), var(--bb-tan));
    display: flex; align-items: center; justify-content: center;
  }
  .op-menu-head b { font-family: var(--bb-font-body); font-weight: 600; font-size: 14px; color: var(--bb-white); }
  .op-menu-head i { font-style: normal; font-family: var(--bb-font-display); font-weight: 700; font-size: 10px; color: var(--bb-tan); }

  .op-menu-section {
    padding: 2px 10px 4px;
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--bb-tan);
  }
  /* Ruled off from Log out below; the Scroller inside caps the height (~4.5
     rows) so a long roster never pushes Log out off-screen. */
  .op-dash-group { margin-bottom: 6px; padding-bottom: 6px; border-bottom: 1px solid var(--bb-border); }
  .op-dash-list { display: flex; flex-direction: column; gap: 2px; }
  .op-dash {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 7px 10px; border-radius: var(--bb-radius-sm);
    text-decoration: none; cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .op-dash:hover { background: rgba(201, 168, 124, 0.1); }
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
  .op-dash:hover .dash-name { color: var(--bb-white); }

  .op-menu form { display: flex; }
  .op-menu-item {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 10px 10px; border-radius: var(--bb-radius-sm);
    background: none; border: none; cursor: pointer; text-decoration: none;
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-muted);
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease;
  }
  .op-menu-item :global(svg) { stroke: currentColor; fill: none; }
  .op-menu-item:hover { color: var(--bb-white); background: rgba(201, 168, 124, 0.1); }

  @media (min-width: 761px) {
    .op-id { display: flex; }
  }
  @media (prefers-reduced-motion: reduce) {
    .op-menu { animation: none; }
  }
</style>
