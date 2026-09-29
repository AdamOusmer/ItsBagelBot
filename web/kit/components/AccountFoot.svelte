<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Bolota from './Bolota.svelte';
  import LogoutForm from './LogoutForm.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { SITE } from '../lib/site-links';
  import { supportLines } from '../lib/support-lines';
  import { menuKeys } from '../lib/menu-keys';

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
  let supportOpen = $state(false);

  const lines = $derived(supportLines(t));
  let supportBtn = $state<HTMLButtonElement | null>(null);
  let accountBtn = $state<HTMLButtonElement | null>(null);

  function closeMenus(restoreFocus = false) {
    const trigger = supportOpen ? supportBtn : menuOpen ? accountBtn : null;
    menuOpen = false;
    supportOpen = false;
    if (restoreFocus) trigger?.focus();
  }

  afterNavigate(() => closeMenus());
</script>

<svelte:window onkeydown={(e) => { if ((menuOpen || supportOpen) && e.key === 'Escape') closeMenus(true); }} />

<div class="bb-profile-rail__side-foot">
  <div class="bb-profile-rail__help">
    <button
      class="bb-profile-rail__help-btn"
      class:bb-profile-rail__open={supportOpen}
      type="button"
      aria-expanded={supportOpen}
      aria-haspopup="menu"
      bind:this={supportBtn}
      onclick={() => { menuOpen = false; supportOpen = !supportOpen; }}
    >
      {t('topbar.support')}
    </button>
    <a class="bb-profile-rail__help-btn" href={SITE.newIssue} target="_blank" rel="noopener noreferrer">
      {t('topbar.feedback')}
    </a>
  </div>
  {#if menuOpen || supportOpen}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="bb-profile__scrim"
      role="presentation"
      onclick={() => closeMenus()}
    ></div>
  {/if}
  {#if supportOpen}
    <div class="bb-profile-rail__foot-menu" role="menu" aria-label={t('topbar.support')} use:menuKeys={() => closeMenus(true)}>
      {#each lines as line (line.href)}
        <a
          class="bb-profile__link"
          href={line.href}
          role="menuitem"
          target={line.external ? '_blank' : undefined}
          rel={line.external ? 'noopener noreferrer' : undefined}
        >
          <span class="bb-profile__glyph"><Icon name={line.icon} size={16} /></span>
          <span class="bb-profile__line">
            <span class="bb-profile__name">{line.title}</span>
            <span class="bb-profile__hint" title={line.hint}>{line.hint}</span>
          </span>
        </a>
      {/each}
    </div>
  {/if}
  {#if hasMenu}
    <button
      class="bb-profile-rail__account bb-profile-rail__account--btn"
      class:bb-profile-rail__open={menuOpen}
      type="button"
      aria-expanded={menuOpen}
      aria-haspopup="menu"
      bind:this={accountBtn}
      onclick={() => { supportOpen = false; menuOpen = !menuOpen; }}
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
      <div class="bb-profile-rail__foot-menu" role="menu" aria-label={name} use:menuKeys={() => closeMenus(true)}>
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
  <LogoutForm />
</div>
