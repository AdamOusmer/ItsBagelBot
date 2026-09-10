<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The Svelte adapter for the button contract. Its Astro twin is
  // ../astro/Button.astro and ui/test/parity.test.ts holds the two to the same
  // markup; the look is entirely ../styles/elements/button.css, which is why
  // this file has no <style> block at all. A scoped copy here is exactly the
  // duplication this package exists to delete: the console, the marketing site
  // and this component each carried one, and they disagreed.
  //
  // Two runtime concerns survive the move, and only two:
  //  1. the icon-variant accessible-name warning below, and
  //  2. `isDisabled`, which makes `loading` set native `disabled` so a submit
  //     cannot be double-fired while the first one is in flight.
  import type { Snippet } from 'svelte';
  import '../styles/elements/button.css';

  let {
    variant = 'primary',
    solid = false,
    block = false,
    size = 'md',
    type = 'button',
    onclick,
    loading = false,
    done = false,
    disabled = false,
    class: cls = '',
    children,
    ...rest
  }: {
    // `tan` and `quiet` are kept as aliases of `secondary`, and `go` of
    // `green`, so the console's and the site's existing call sites keep
    // working; button.css defines all three as the same rule.
    variant?: 'primary' | 'secondary' | 'ghost' | 'green' | 'destructive' | 'icon' | 'tan' | 'quiet' | 'go';
    // Only meaningful with `green`: the nav CTA is the one filled button in
    // the system.
    solid?: boolean;
    /** Full width, centred, >=44px tall: the mobile action-row shape. */
    block?: boolean;
    size?: 'md' | 'sm';
    type?: 'button' | 'submit' | 'reset';
    onclick?: (e: MouseEvent) => void;
    loading?: boolean;
    done?: boolean;
    disabled?: boolean;
    class?: string;
    // Optional: the `icon` variant is icon-only, so it has no children.
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  // Icon-only buttons carry no text, so they MUST be given an accessible name
  // by the caller (aria-label / aria-labelledby via ...rest). Warn, never
  // crash: a nameless button is a bug, but blanking the page over it is worse.
  // The Astro twin cannot warn at runtime and throws at build time instead.
  $effect(() => {
    if (variant === 'icon' && !rest['aria-label'] && !rest['aria-labelledby']) {
      console.warn('[Button] variant="icon" needs an aria-label (icon-only button has no text).');
    }
  });

  // While loading we set native `disabled`, which both blocks a second submit
  // and drops the button out of the tab order for the duration. Deliberate:
  // aria-busy alone leaves a focusable control that does nothing.
  const isDisabled = $derived(disabled || loading);

  // Built as a list rather than an interpolated template so the Astro twin can
  // build the identical string the identical way. State is a class, not a
  // `data-state`, because the raw HTML call sites that predate the adapters
  // already write `.is-loading` / `.is-done` and one state channel is enough.
  const classes = $derived(
    [
      'bb-btn',
      `bb-btn--${variant}`,
      solid && 'bb-btn--solid',
      block && 'bb-btn--block',
      size === 'sm' && 'bb-btn--sm',
      loading && 'is-loading',
      done && 'is-done',
      cls,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<!-- Attribute ORDER matches ../astro/Button.astro on purpose: the parity test
     diffs the rendered HTML and deliberately does not sort attributes, so that
     the two adapters stay readable side by side. -->
<button
  class={classes}
  {type}
  disabled={isDisabled}
  aria-busy={loading ? 'true' : undefined}
  data-mark=""
  {onclick}
  {...rest}
>
  {#if variant !== 'icon'}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{#if children}{@render children()}{/if}</span>
  {#if loading}<span class="bb-btn__spinner" aria-hidden="true"></span>{/if}
</button>
