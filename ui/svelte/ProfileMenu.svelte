<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';

  import type { IconName } from '../lib/icons';

  export interface ProfileMenuLink {
    href: string;
    label: string;
    hint?: string;
    icon?: IconName;
    external?: boolean;
  }

  export interface ProfileAvatar {
    name: string;
    size: number;
    active: boolean;
    item: boolean;
  }
</script>

<script lang="ts">
  import '../styles/elements/profile-menu.css';
  import type { Snippet } from 'svelte';
  import { menuKeys } from '../lib/menu-keys';
  import { hasOpenOverlay } from '../lib/overlay-stack';
  import Button from './Button.svelte';
  import Icon from './Icon.svelte';
  import Scroller from './Scroller.svelte';
  import VisuallyHidden from './VisuallyHidden.svelte';

  type Own = {
    variant?: 'topbar' | 'rail';
    name: string;
    caption: string;
    open?: boolean;
    helpOpen?: boolean;
    links?: ProfileMenuLink[];
    linksLabel?: string;
    exit?: ProfileMenuLink;
    logoutLabel: string;
    logoutAction?: string;
    onlogout?: () => void;
    help?: ProfileMenuLink[];
    helpLabel?: string;
    helpTitle?: string;
    more?: ProfileMenuLink[];
    newTabLabel?: string;
    feedback?: ProfileMenuLink;
    menuLabel?: string;
    triggerLabel?: string;
    avatar: Snippet<[ProfileAvatar]>;
    class?: string;
  };

  let {
    variant = 'topbar',
    name,
    caption,
    open = $bindable(false),
    helpOpen = $bindable(false),
    links = [],
    linksLabel = '',
    exit,
    logoutLabel,
    logoutAction = '/auth/logout',
    onlogout,
    help = [],
    helpLabel = '',
    helpTitle = '',
    more = [],
    newTabLabel = '',
    feedback,
    menuLabel,
    triggerLabel,
    avatar,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  let hovered = $state(false);
  let topTrigger = $state<HTMLButtonElement | null>(null);
  let railTrigger = $state<HTMLButtonElement | null>(null);
  let helpTrigger = $state<HTMLButtonElement | null>(null);

  const trigger = $derived(triggerLabel ?? `${name} · ${caption}`);
  const hasMenu = $derived(links.length > 0 || Boolean(exit));
  const hasHelp = $derived(help.length > 0 || Boolean(feedback));
  const hasTopbarHelp = $derived(help.length > 0 || more.length > 0);

  function toggleAccount() {
    helpOpen = false;
    open = !open;
  }

  function toggleHelp() {
    open = false;
    helpOpen = !helpOpen;
  }

  function close() {
    open = false;
    helpOpen = false;
  }

  function closeOnEnter(event: KeyboardEvent) {
    if (event.key === 'Enter') close();
  }

  function opener() {
    if (helpOpen) return helpTrigger;
    if (!open) return null;
    return variant === 'topbar' ? topTrigger : railTrigger;
  }

  function closeToTrigger() {
    const trigger = opener();
    close();
    trigger?.focus();
  }

  function onkeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape' || hasOpenOverlay()) return;
    closeToTrigger();
  }

  const hover = (on: boolean) => () => (hovered = on);
</script>

<svelte:window {onkeydown} />

{#snippet scrim()}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="bb-profile__scrim" role="presentation" onclick={close} onkeydown={closeOnEnter}></div>
{/snippet}

{#snippet head()}
  <div class="bb-profile__head">
    <span class="bb-profile__portrait">{@render avatar({ name, size: 72, active: open, item: false })}</span>
    <b>{name}</b>
    <i>{caption}</i>
  </div>
{/snippet}

{#snippet list()}
  <div class="bb-profile__section">{linksLabel}</div>
  <Scroller maxHeight="208px" role="group" aria-label={linksLabel}>
    <div class="bb-profile__list">
      {#each links as link (link.href)}
        <a class="bb-profile__link" href={link.href} role="menuitem" onclick={close}>
          <span class="bb-profile__avatar">{@render avatar({ name: link.label, size: 26, active: open, item: true })}</span>
          <span class="bb-profile__name">{link.label}</span>
        </a>
      {/each}
    </div>
  </Scroller>
{/snippet}

{#snippet exitLink(link: ProfileMenuLink)}
  <a class="bb-profile__link" href={link.href} role="menuitem" onclick={close}>
    <span class="bb-profile__avatar"><Icon name={link.icon ?? 'home'} size={14} /></span>
    <span class="bb-profile__name">{link.label}</span>
  </a>
{/snippet}

{#snippet helpLine(line: ProfileMenuLink)}
  <a
    class="bb-profile__link"
    href={line.href}
    role="menuitem"
    target={line.external ? '_blank' : undefined}
    rel={line.external ? 'noopener noreferrer' : undefined}
    onclick={close}
  >
    {#if line.icon}<span class="bb-profile__glyph"><Icon name={line.icon} size={16} /></span>{/if}
    <span class="bb-profile__line">
      <span class="bb-profile__name">{line.label}</span>
      {#if line.hint}<span class="bb-profile__hint" title={line.hint}>{line.hint}</span>{/if}
    </span>
  </a>
{/snippet}

{#snippet moreLine(line: ProfileMenuLink)}
  <a
    class="bb-profile__link"
    href={line.href}
    role="menuitem"
    target={line.external ? '_blank' : undefined}
    rel={line.external ? 'noopener noreferrer' : undefined}
    onclick={close}
  >
    {#if line.icon}<span class="bb-profile__glyph"><Icon name={line.icon} size={16} /></span>{/if}
    <span class="bb-profile__name"
      >{line.label}{#if line.external && newTabLabel}{' '}<VisuallyHidden>{newTabLabel}</VisuallyHidden>{/if}</span
    >
  </a>
{/snippet}

{#if variant === 'topbar'}
  <button
    class={['bb-profile-topbar__operator', className || null].filter(Boolean).join(' ')}
    class:bb-profile-topbar__open={open}
    type="button"
    title={trigger}
    aria-label={trigger}
    aria-expanded={open}
    aria-haspopup="menu"
    bind:this={topTrigger}
    onclick={toggleAccount}
    onpointerenter={hover(true)}
    onpointerleave={hover(false)}
    {...rest}
  >
    <span class="bb-profile-topbar__avatar">{@render avatar({ name, size: 30, active: hovered || open, item: false })}</span>
    <span class="bb-profile-topbar__op-id"><b>{name}</b><i>{caption}</i></span>
  </button>
  {#if open}
    {@render scrim()}
    <div class="bb-profile-topbar__op-menu" role="menu" aria-label={menuLabel} use:menuKeys={closeToTrigger}>
      {@render head()}
      {#if links.length}<div class="bb-profile-topbar__op-dash-group">{@render list()}</div>{/if}
      {#if exit}<div class="bb-profile-topbar__op-dash-group">{@render exitLink(exit)}</div>{/if}
      {#if hasTopbarHelp}
        <div class="bb-profile-topbar__op-dash-group">
          <div class="bb-profile__section">{helpTitle}</div>
          {#each help as line (line.href)}{@render helpLine(line)}{/each}
          {#each more as line (line.href)}{@render moreLine(line)}{/each}
        </div>
      {/if}
      <form method="POST" action={logoutAction} onsubmit={onlogout}>
        <button type="submit" class="bb-profile-topbar__op-menu-item" role="menuitem">{logoutLabel}</button>
      </form>
    </div>
  {/if}
{:else}
  <div class={['bb-profile-rail__side-foot', className || null].filter(Boolean).join(' ')} {...rest}>
    {#if hasHelp}
      <div class="bb-profile-rail__help">
        {#if help.length}
          <button
            class="bb-profile-rail__help-btn"
            class:bb-profile-rail__open={helpOpen}
            type="button"
            aria-expanded={helpOpen}
            aria-haspopup="menu"
            bind:this={helpTrigger}
            onclick={toggleHelp}>{helpLabel}</button
          >
        {/if}
        {#if feedback}
          <a
            class="bb-profile-rail__help-btn"
            href={feedback.href}
            target={feedback.external ? '_blank' : undefined}
            rel={feedback.external ? 'noopener noreferrer' : undefined}>{feedback.label}</a
          >
        {/if}
      </div>
    {/if}
    {#if open || helpOpen}{@render scrim()}{/if}
    {#if helpOpen}
      <div class="bb-profile-rail__foot-menu" role="menu" aria-label={helpLabel} use:menuKeys={closeToTrigger}>
        {#each help as line (line.href)}{@render helpLine(line)}{/each}
      </div>
    {/if}
    {#if hasMenu}
      <button
        class="bb-profile-rail__account bb-profile-rail__account--btn"
        class:bb-profile-rail__open={open}
        type="button"
        aria-expanded={open}
        aria-haspopup="menu"
        bind:this={railTrigger}
        onclick={toggleAccount}
        onpointerenter={hover(true)}
        onpointerleave={hover(false)}
      >
        <span class="bb-profile-rail__avatar">{@render avatar({ name, size: 34, active: hovered || open, item: false })}</span>
        <span class="bb-profile-rail__who"><b>{name}</b><span>{caption}</span></span>
        <span class="bb-profile-rail__chev" class:bb-profile-rail__open={open} aria-hidden="true"
          ><Icon name="chevron" size={14} /></span
        >
      </button>
      {#if open}
        <div class="bb-profile-rail__foot-menu" role="menu" aria-label={menuLabel} use:menuKeys={closeToTrigger}>
          {@render head()}
          {#if links.length}{@render list()}{/if}
          {#if exit}{@render exitLink(exit)}{/if}
        </div>
      {/if}
    {:else}
      <div class="bb-profile-rail__account" role="group" onpointerenter={hover(true)} onpointerleave={hover(false)}>
        <div class="bb-profile-rail__avatar">{@render avatar({ name, size: 34, active: hovered, item: false })}</div>
        <div class="bb-profile-rail__who"><b>{name}</b><span>{caption}</span></div>
      </div>
    {/if}
    <form class="bb-profile-rail__logout" method="POST" action={logoutAction} onsubmit={onlogout}>
      <Button variant="ghost" type="submit">{logoutLabel}</Button>
    </form>
  </div>
{/if}
