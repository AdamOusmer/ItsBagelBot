<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/button.css';

  let {
    href,
    variant = 'primary',
    solid = false,
    block = false,
    size = 'md',
    done = false,
    class: cls = '',
    children,
    ...rest
  }: {
    href: string;
    variant?: 'primary' | 'secondary' | 'ghost' | 'green' | 'destructive' | 'icon' | 'tan' | 'quiet' | 'go';
    solid?: boolean;
    block?: boolean;
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
      block && 'bb-btn--block',
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
