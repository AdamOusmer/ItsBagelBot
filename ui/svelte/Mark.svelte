<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/mark.css';

  type Own = {
    variant?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    size?: string;
    style?: string;
    class?: string;
  };

  let {
    variant = 'solid',
    size,
    style,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['i'], keyof Own> = $props();

  const classes = $derived(
    ['bb-mark', variant === 'solid' ? null : `bb-mark--${variant}`, className || null].filter(Boolean).join(' '),
  );
  const styles = $derived([size && `--mark-size: ${size};`, style].filter(Boolean).join(' ') || undefined);
</script>

<i class={classes} style={styles} aria-hidden="true" {...rest}></i>
