<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // A link that LOOKS like a button, for navigation. A real <a href>, so it
  // keeps native link semantics (open in a new tab, right-click, "link" to a
  // screen reader). Never a <button> faking navigation.
  //
  // Two Svelte files where Astro has one (divergence BL1): the Astro adapter
  // switches on `href` and renders <a> or <button> from one component, which
  // Svelte cannot do without <svelte:element>, and <svelte:element> loses the
  // href-specific typing that is the whole reason to reach for this component
  // instead of Button. ONE stylesheet either way -- the copy of Button's
  // scoped CSS this file used to hand-keep (and which had already drifted: it
  // was missing --btn-spinner and the reduced-motion block) is gone.
  //
  // No `loading` / `disabled`: a link cannot be busy and cannot be disabled.
  // `aria-disabled` via ...rest is the escape hatch, and button.css's hover
  // guard honours it.
  import type { Snippet } from 'svelte';
  import '../styles/elements/button.css';

  let {
    href,
    variant = 'primary',
    solid = false,
    size = 'md',
    done = false,
    class: cls = '',
    children,
    ...rest
  }: {
    href: string;
    variant?: 'primary' | 'secondary' | 'ghost' | 'green' | 'destructive' | 'icon' | 'tan' | 'quiet' | 'go';
    solid?: boolean;
    size?: 'md' | 'sm';
    done?: boolean;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  $effect(() => {
    if (variant === 'icon' && !rest['aria-label'] && !rest['aria-labelledby']) {
      console.warn('[ButtonLink] variant="icon" needs an aria-label (icon-only link has no text).');
    }
  });

  const classes = $derived(
    [
      'bb-btn',
      `bb-btn--${variant}`,
      solid && 'bb-btn--solid',
      size === 'sm' && 'bb-btn--sm',
      done && 'is-done',
      cls,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<a class={classes} {href} data-mark="" {...rest}>
  {#if variant !== 'icon'}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{#if children}{@render children()}{/if}</span>
</a>
