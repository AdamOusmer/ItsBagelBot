<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/alert.css';
  import type { Snippet } from 'svelte';

  let {
    variant = 'danger',
    role = 'alert',
    class: className = '',
    children,
    action,
    ...rest
  }: {
    variant?: 'danger' | 'warn' | 'impersonation';
    role?: 'alert' | 'status';
    class?: string;
    children?: Snippet;
    action?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-alert', `bb-alert--${variant}`, className || null].filter(Boolean).join(' '),
  );
</script>

<div class={classes} {role} {...rest}><span class="bb-alert__msg"
    >{#if children}{@render children()}{/if}</span
  >{#if action}{@render action()}{/if}</div>
