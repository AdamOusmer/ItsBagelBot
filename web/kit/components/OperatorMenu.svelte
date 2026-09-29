<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Bolota from './Bolota.svelte';
  import LogoutForm from './LogoutForm.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';
  import { SITE } from '../lib/site-links';
  import { supportLines } from '../lib/support-lines';
  import { menuKeys } from '../lib/menu-keys';

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
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let menuOpen = $state(false);
  let trigger = $state<HTMLButtonElement | null>(null);

  let hovered = $state(false);

  const lines = $derived(supportLines(t));
  const helpLinks = $derived([
    { label: t('topbar.feedback'), href: SITE.newIssue, icon: 'edit' },
    { label: t('nav.status'), href: SITE.status, icon: 'pulse' }
  ] as const);

  function close(restoreFocus = false) {
    menuOpen = false;
    if (restoreFocus) trigger?.focus();
  }

  afterNavigate(() => close());
</script>

<svelte:window onkeydown={(e) => { if (menuOpen && e.key === 'Escape') close(true); }} />

<button
  class="bb-profile-topbar__operator"
  class:bb-profile-topbar__open={menuOpen}
  title="{accountName} · {accountRole}"
  aria-label="{accountName} · {accountRole}"
  aria-expanded={menuOpen}
  aria-haspopup="menu"
  bind:this={trigger}
  onclick={() => (menuOpen = !menuOpen)}
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  <span class="bb-profile-topbar__avatar"><Bolota name={accountName} size={30} active={hovered || menuOpen} /></span>
  <span class="bb-profile-topbar__op-id">
    <b>{accountName}</b>
    <i>{accountRole}</i>
  </span>
</button>
{#if menuOpen}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="bb-profile__scrim"
    role="presentation"
    onclick={() => close()}
  ></div>
  <div class="bb-profile-topbar__op-menu" role="menu" aria-label="{accountName} · {accountRole}" use:menuKeys={() => close(true)}>
    <div class="bb-profile__head">
      <span class="bb-profile__portrait">
        <Bolota name={accountName} size={72} active={menuOpen} />
      </span>
      <b>{accountName}</b>
      <i>{accountRole}</i>
    </div>
    {#if dashboards.length}
      <div class="bb-profile-topbar__op-dash-group">
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
      </div>
    {/if}
    {#if isDelegate}
      <div class="bb-profile-topbar__op-dash-group">
        <a class="bb-profile__link" href={delegateExitHref} role="menuitem">
          <span class="bb-profile__avatar"><Icon name="home" size={14} /></span>
          <span class="bb-profile__name">{delegateExitLabel}</span>
        </a>
      </div>
    {/if}
    <div class="bb-profile-topbar__op-dash-group bb-op-help">
      <div class="bb-profile__section">{t('topbar.supportTitle')}</div>
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
      {#each helpLinks as link (link.href)}
        <a class="bb-profile__link" href={link.href} role="menuitem" target="_blank" rel="noopener noreferrer">
          <span class="bb-profile__glyph"><Icon name={link.icon} size={16} /></span>
          <span class="bb-profile__name">{link.label}<span class="bb-sr-only"> {t('common.opensInNewTab')}</span></span>
        </a>
      {/each}
    </div>
    <LogoutForm menu />
  </div>
{/if}
