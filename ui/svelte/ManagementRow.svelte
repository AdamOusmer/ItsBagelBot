<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/management-row.css';
  import type { Snippet } from 'svelte';

  type Own = {
    as?: string;
    href?: string;
    selectable?: boolean;
    selected?: boolean;
    expanded?: boolean;
    controls?: string;
    disabled?: boolean;
    accent?: boolean;
    wrap?: boolean;
    stackActions?: boolean;
    label?: string;
    title?: string;
    meta?: string;
    class?: string;
    onSelect?: () => void;
    leading?: Snippet;
    badge?: Snippet;
    marks?: Snippet;
    primary?: Snippet;
    actions?: Snippet;
  };

  let {
    as = 'div',
    href,
    selectable = true,
    selected = false,
    expanded = false,
    controls,
    disabled = false,
    accent = false,
    wrap = false,
    stackActions = false,
    label,
    title,
    meta,
    class: className = '',
    onSelect,
    leading,
    badge,
    marks,
    primary,
    actions,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const isStatic = $derived(!href && !selectable);

  const classes = $derived(
    [
      'bb-row',
      'row-shell',
      accent ? 'bb-row--accent' : null,
      isStatic ? 'bb-row--static' : null,
      selected ? 'is-selected' : null,
      disabled ? 'is-off' : null,
      wrap ? 'bb-row--wrap' : null,
      stackActions ? 'bb-row--stack-actions' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );

  function primaryElement() {
    const current: 'true' | undefined = selected ? 'true' : undefined;
    if (href) {
      return { tag: 'a', attrs: { href, 'data-cursor': 'quiet', 'aria-current': current, 'aria-label': label } };
    }
    if (isStatic) return { tag: 'div', attrs: { 'data-cursor': 'quiet' } };
    return {
      tag: 'button',
      attrs: {
        type: 'button',
        'data-cursor': 'quiet',
        'aria-expanded': expanded,
        'aria-controls': controls,
        'aria-current': current,
        'aria-label': label,
      },
    };
  }

  const primaryEl = $derived(primaryElement());
</script>

{#snippet line()}
  <span class="bb-row__line">
    {#if leading}{@render leading()}{/if}
    <span class="bb-row__text">
      <span class="bb-row__title">{title}{#if badge}<span class="bb-row__badge">{@render badge()}</span>{/if}</span>
      {#if meta}<span class="bb-row__meta">{meta}</span>{/if}
    </span>
    {#if primary}{@render primary()}{/if}
    {#if marks}<span class="bb-row__marks">{@render marks()}</span>{/if}
  </span>
{/snippet}

<svelte:element this={as} class={classes} {...rest}>
  <svelte:element this={primaryEl.tag} class="bb-row__primary" {...primaryEl.attrs} onclick={onSelect}
    >{#if title !== undefined}{@render line()}{:else if primary}{@render primary()}{/if}</svelte:element
  >
  {#if actions}<div class="bb-row__actions">{@render actions()}</div>{/if}
</svelte:element>
