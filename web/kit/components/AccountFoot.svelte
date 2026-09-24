<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Bolota from './Bolota.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

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
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let hovered = $state(false);

  const hasMenu = $derived(dashboards.length > 0 || isDelegate);
  let menuOpen = $state(false);

  afterNavigate(() => (menuOpen = false));
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<div class="bb-profile-rail__side-foot">
  {#if hasMenu}
    <button
      class="bb-profile-rail__account bb-profile-rail__account--btn"
      class:bb-profile-rail__open={menuOpen}
      type="button"
      aria-expanded={menuOpen}
      aria-haspopup="menu"
      onclick={() => (menuOpen = !menuOpen)}
      onpointerenter={() => (hovered = true)}
      onpointerleave={() => (hovered = false)}
    >
      <span class="bb-profile-rail__avatar"><Bolota name={name} size={34} active={hovered || menuOpen} /></span>
      <span class="bb-profile-rail__who">
        <b>{name}</b>
        <span>{role}</span>
      </span>
      <span class="bb-profile-rail__chev" class:bb-profile-rail__open={menuOpen} aria-hidden="true">
        <svg viewBox="0 0 24 24" width="14" height="14"><polyline points="9 6 15 12 9 18"></polyline></svg>
      </span>
    </button>
    {#if menuOpen}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="bb-profile__scrim"
        role="presentation"
        onclick={() => (menuOpen = false)}
        onkeydown={(e) => { if (e.key === 'Enter') menuOpen = false; }}
      ></div>
      <div class="bb-profile-rail__foot-menu" role="menu">
        <div class="bb-profile__head">
          <span class="bb-profile__portrait">
            <Bolota name={name} size={72} active={menuOpen} />
          </span>
          <b>{name}</b>
          <i>{role}</i>
        </div>
        {#if dashboards.length}
          <div class="bb-profile__section">{t('topbar.dashboards')}</div>
          <Scroller maxHeight="208px" role="group" aria-label={t('topbar.dashboards')}>
            <div class="bb-profile__list">
              {#each dashboards as d (d.href)}
                <a class="bb-profile__link" href={d.href} role="menuitem">
                  <span class="bb-profile__avatar"><Bolota name={d.name} size={26} active={menuOpen} gate /></span>
                  <span class="bb-profile__name">{d.name}</span>
                </a>
              {/each}
            </div>
          </Scroller>
        {/if}
        {#if isDelegate}
          <a class="bb-profile__link" href={delegateExitHref} role="menuitem">
            <span class="bb-profile__avatar"><Icon name="home" size={14} /></span>
            <span class="bb-profile__name">{delegateExitLabel}</span>
          </a>
        {/if}
      </div>
    {/if}
  {:else}
    <div class="bb-profile-rail__account" role="group" onpointerenter={() => (hovered = true)} onpointerleave={() => (hovered = false)}>
      <div class="bb-profile-rail__avatar"><Bolota name={name} size={34} active={hovered} /></div>
      <div class="bb-profile-rail__who">
        <b>{name}</b>
        <span>{role}</span>
      </div>
    </div>
  {/if}
  <form method="POST" action="/auth/logout" onsubmit={() => localStorage.removeItem('bb-onboarded')}>
    <Button variant="ghost" type="submit" style="width:100%;justify-content:center;margin-top:10px">
      {t('topbar.logout')}
    </Button>
  </form>
</div>
