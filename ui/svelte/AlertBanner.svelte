<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-alert` contract (../styles/elements/alert.css).
  // Its Astro twin is ../astro/AlertBanner.astro and ../test/parity.test.ts
  // holds the two to identical markup.
  //
  // The inline band a page puts above its content when the data behind it is
  // degraded, stale, or refused. Message in the default snippet; an optional
  // `action` snippet trails on the right.
  //
  // role="alert" and not "status" by default: these announce a failure the
  // visitor has to act on (a service is down, a write was refused), which is
  // what the assertive live region is for. The impersonation bar passes
  // role="status" instead — it is a standing condition, not an event, and an
  // assertive region that is present on every route interrupts every one of
  // them.
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
    /**
     * danger = the red refusal band; warn = the tan advisory;
     * impersonation = the fixed, inverted staff bar.
     */
    variant?: 'danger' | 'warn' | 'impersonation';
    /** ARIA live-region role. `status` for a standing condition. */
    role?: 'alert' | 'status';
    class?: string;
    children?: Snippet;
    /** Trailing content: a retry button, a link to the status page. */
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
