<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-text` contract
  // (../styles/elements/typography.css). Astro twin: ../astro/Text.astro.
  //
  // The tone class is emitted only when it is NOT the default, while the size
  // class is always emitted. Not an inconsistency: `.bb-text` already carries
  // the default colour, so `.bb-text--default` would be a no-op class on the
  // majority of the text in three apps, whereas the size class is what a
  // reader of the markup uses to tell 13px caption from 16px body without
  // opening the stylesheet.
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    size = 'md',
    tone = 'default',
    mono = false,
    as: tag = 'p',
    class: className = '',
    children,
    ...rest
  }: {
    size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl';
    tone?: 'default' | 'muted' | 'accent' | 'danger';
    /** Renders in the mono face, stepped down to match the body x-height. */
    mono?: boolean;
    as?: 'p' | 'span' | 'small' | 'div' | 'li' | 'dd' | 'dt';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-text',
      `bb-text--${size}`,
      tone === 'default' ? null : `bb-text--${tone}`,
      mono ? 'bb-text--mono' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
