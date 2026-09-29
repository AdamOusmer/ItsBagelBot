<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/alert.css';
  import type { Snippet } from 'svelte';

  let {
    variant = 'danger',
    role = 'alert',
    callout = false,
    flush = false,
    stack = false,
    second = false,
    exit,
    class: className = '',
    children,
    action,
    ...rest
  }: {
    variant?: 'danger' | 'warn' | 'impersonation' | 'tip' | 'note' | 'positive';
    role?: 'alert' | 'status' | 'note';
    callout?: boolean;
    flush?: boolean;
    stack?: boolean;
    second?: boolean;
    exit?: { label: string; href?: string; action?: string };
    class?: string;
    children?: Snippet;
    action?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-alert',
      `bb-alert--${variant}`,
      callout ? 'bb-alert--callout' : null,
      flush ? 'bb-alert--flush' : null,
      stack ? 'bb-alert--stack' : null,
      second ? 'bb-alert--row-2' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {role} {...rest}><span class="bb-alert__msg"
    >{#if children}{@render children()}{/if}</span
  >{#if action}{@render action()}{/if}{#if exit?.action}<form method="POST" action={exit.action}
      ><button type="submit" class="bb-alert__exit">{exit.label}</button></form
    >{:else if exit}<a class="bb-alert__exit" href={exit.href}>{exit.label}</a>{/if}</div>
