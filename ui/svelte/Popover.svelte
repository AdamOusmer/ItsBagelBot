<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/popover.css';
  import type { Snippet } from 'svelte';
  import { hasOpenOverlay, overlayContains } from '../lib/overlay-stack';
  import Heading from './Heading.svelte';
  import Icon from './Icon.svelte';

  type Own = {
    open?: boolean;
    label: string;
    title: string;
    closeLabel: string;
    expands?: boolean;
    placement?: 'top' | 'bottom';
    onactivate?: () => void;
    dismissLabel?: string;
    ondismiss?: () => void;
    pill: Snippet;
    class?: string;
    children?: Snippet;
  };

  let {
    open = $bindable(false),
    label,
    title,
    closeLabel,
    expands = true,
    placement = 'top',
    onactivate,
    dismissLabel = '',
    ondismiss,
    pill,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const uid = $props.id();
  const titleId = `bb-popover-title-${uid}`;
  const classes = $derived(['bb-popover', placement === 'bottom' ? 'bb-popover--bottom' : null, className || null].filter(Boolean).join(' '));

  let root = $state<HTMLDivElement>();
  let closeButton = $state<HTMLButtonElement>();
  let cta = $state<HTMLButtonElement>();
  let wasOpen = false;

  $effect.pre(() => {
    const closing = wasOpen && !open;
    wasOpen = open;
    if (closing && root?.contains(document.activeElement)) cta?.focus();
  });

  $effect(() => {
    if (open) closeButton?.focus();
  });

  function activate() {
    if (expands) open = !open;
    else onactivate?.();
  }

  function close() {
    open = false;
  }

  function onkeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape' || hasOpenOverlay()) return;
    close();
  }

  function onpointerdown(event: PointerEvent) {
    if (!open || !root) return;
    if (!overlayContains(root, event.target as Node | null)) close();
  }
</script>

<svelte:window {onkeydown} />
<svelte:document {onpointerdown} />

<div class={classes} bind:this={root} {...rest}>
  <div class="bb-popover__pill">
    <button
      class="bb-popover__cta"
      bind:this={cta}
      type="button"
      aria-label={label}
      aria-haspopup={expands ? 'dialog' : undefined}
      aria-expanded={expands ? open : undefined}
      onclick={activate}>{@render pill()}</button
    >
    {#if ondismiss}
      <button class="bb-popover__x" type="button" aria-label={dismissLabel} onclick={ondismiss}>
        <Icon name="x" size={13} />
      </button>
    {/if}
  </div>

  {#if open}
    <div class="bb-popover__sheet" role="dialog" aria-modal="false" aria-labelledby={titleId}>
      <div class="bb-popover__head">
        <Heading level={6} as="h2" id={titleId}>{title}</Heading>
        <button
          class="bb-popover__close"
          type="button"
          aria-label={closeLabel}
          onclick={close}
          bind:this={closeButton}
        >
          <Icon name="x" size={14} />
        </button>
      </div>
      {#if children}{@render children()}{/if}
    </div>
  {/if}
</div>
