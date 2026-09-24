<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/icon.css';
  import { icons, type IconName } from '../lib/icons';

  let {
    name,
    size = 16,
    strokeWidth = 1.6,
    fill = 'none',
    class: className = '',
    ...rest
  }: {
    name: IconName;
    size?: number;
    strokeWidth?: number;
    fill?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-icon', className || null].filter(Boolean).join(' '));
</script>

<!-- {@html} is safe only because the bodies are generated constants, never user input. -->
<svg
  class={classes}
  viewBox="0 0 24 24"
  width={size}
  height={size}
  {fill}
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  {...rest}>{@html icons[name]}</svg
>
