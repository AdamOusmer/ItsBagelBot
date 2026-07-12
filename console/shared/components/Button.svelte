<script lang="ts">
  // The canonical button. Raw `.btn` (app.css) still exists for pages that use
  // it directly; this component is the a11y-complete version: native disabled,
  // aria-busy loading with no width shift, and a hard guard against double
  // submits while a request is in flight.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    variant = 'ghost',
    icon,
    type = 'button',
    onclick,
    loading = false,
    disabled = false,
    class: cls = '',
    children,
    ...rest
  }: {
    // `tan` is kept as an alias of `secondary` so existing callers don't break.
    variant?: 'primary' | 'secondary' | 'ghost' | 'destructive' | 'icon' | 'tan';
    icon?: IconName;
    type?: 'button' | 'submit';
    onclick?: (e: MouseEvent) => void;
    loading?: boolean;
    disabled?: boolean;
    class?: string;
    // Optional: the `icon` variant is icon-only, so it has no children.
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  // Icon-only buttons carry no text, so they MUST be given an accessible name by
  // the caller (aria-label / aria-labelledby via ...rest). Warn, never crash.
  $effect(() => {
    if (variant === 'icon' && !rest['aria-label'] && !rest['aria-labelledby']) {
      console.warn('[Button] variant="icon" needs an aria-label (icon-only button has no text).');
    }
  });

  // While loading we set native `disabled`, which both blocks a second submit
  // and drops the button out of the tab order for the duration.
  const isDisabled = $derived(disabled || loading);
</script>

<button
  class="btn {variant} {cls}"
  class:is-loading={loading}
  {type}
  {onclick}
  disabled={isDisabled}
  aria-busy={loading || undefined}
  {...rest}
>
  <span class="btn-content">
    {#if icon}<Icon name={icon} size={14} />{/if}
    {#if children}{@render children()}{/if}
  </span>
  {#if loading}<span class="spinner" aria-hidden="true"></span>{/if}
</button>

<style>
  .btn { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase;
    padding: 12px 20px; border-radius: var(--bb-radius-pill); cursor: pointer; white-space: nowrap; position: relative;
    border: 1px solid var(--glass-border); transition: all var(--bb-dur-base) var(--bb-ease-out-back); display: inline-flex; align-items: center; justify-content: center; gap: 8px; }
  .btn :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.8; }
  .btn-content { display: inline-flex; align-items: center; justify-content: center; gap: 8px; }

  .btn.primary { background: var(--ui-accent); color: var(--bb-white); border-color: var(--ui-accent-light); }
  .btn.primary:hover { background: var(--ui-accent-light); box-shadow: 0 0 24px rgba(82,183,136,0.32); }
  .btn.ghost { background: rgba(255,255,255,0.03); color: var(--bb-tan-light); }
  .btn.ghost:hover { background: rgba(201,168,124,0.08); color: var(--bb-tan-pale); border-color: var(--bb-border-strong); }
  /* secondary is the warm tan fill; `tan` kept as an identical alias. */
  .btn.secondary, .btn.tan { background: var(--bb-tan); color: #0a0a0a; border-color: var(--bb-tan-light); }
  .btn.secondary:hover, .btn.tan:hover { background: var(--bb-tan); color: var(--bb-green); border-color: var(--bb-green-light); box-shadow: 0 0 24px rgba(82,183,136,0.3); }
  /* destructive uses the status-error triad so it reads the same everywhere. */
  .btn.destructive { background: var(--bb-status-error-bg, #2a1310); color: var(--bb-status-error-fg, #f0b0a4); border-color: var(--bb-status-error-border, #b05a46); }
  .btn.destructive:hover { background: #3a1a15; box-shadow: 0 0 18px rgba(176, 90, 70, 0.28); }

  /* icon-only: a compact square with a >=44px hit target via padding. */
  .btn.icon { padding: 12px; min-width: 44px; min-height: 44px; }

  .btn:disabled { opacity: 0.5; cursor: not-allowed; box-shadow: none; }
  .btn.is-loading { cursor: progress; }

  /* Loading swaps the label for a centered spinner IN PLACE: the content stays
     laid out (so the button keeps its exact width/height) but is hidden, and the
     spinner is absolutely centered over it. aria-busy carries the state to AT. */
  .btn.is-loading .btn-content { visibility: hidden; }
  .spinner {
    position: absolute;
    top: 50%; left: 50%;
    width: 15px; height: 15px;
    margin: -7.5px 0 0 -7.5px;
    border: 2px solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: btn-spin 0.6s linear infinite;
  }
  @keyframes btn-spin { to { transform: rotate(360deg); } }
  /* Reduced motion freezes the spin; disabled state + aria-busy still convey it. */
  @media (prefers-reduced-motion: reduce) {
    .spinner { animation: none; }
  }
</style>
