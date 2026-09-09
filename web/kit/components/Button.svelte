<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The canonical button. Raw `.btn` (app.css) still exists for pages that use
  // it directly; this component is the a11y-complete version: native disabled,
  // aria-busy loading with no width shift, and a hard guard against double
  // submits while a request is in flight.
  import type { Snippet } from 'svelte';
  let {
    variant = 'ghost',
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
    // `tan` is kept as an alias of `secondary` so existing callers don't break.
    variant?: 'primary' | 'secondary' | 'ghost' | 'destructive' | 'icon' | 'tan';
    size?: 'md' | 'sm';
    type?: 'button' | 'submit';
    onclick?: (e: MouseEvent) => void;
    loading?: boolean;
    done?: boolean;
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
  class:sm={size === 'sm'}
  class:is-loading={loading}
  class:is-done={done}
  {type}
  {onclick}
  disabled={isDisabled}
  aria-busy={loading || undefined}
  {...rest}
  data-mark=""
>
  {#if variant !== 'icon'}<i class="mark" aria-hidden="true"></i>{/if}
  <span class="btn-content">
    {#if children}{@render children()}{/if}
  </span>
  {#if loading}<span class="spinner" aria-hidden="true"></span>{/if}
</button>

<style>
  /* Nothing is filled at rest: a 1px frame plus a 5px rotated-square mark that
     inherits currentColor, so on primary hover it inverts with the fill. */
  .btn { font-family: var(--bb-font-mono); font-size: 12.5px; letter-spacing: 0.04em; line-height: 1;
    padding: 13px 20px 13px 16px; border-radius: var(--bb-radius-sm); cursor: pointer; white-space: nowrap; position: relative;
    background: transparent; border: 1px solid transparent; --btn-spinner: var(--bb-tan-light);
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); display: inline-flex; align-items: center; gap: 12px; }
  .btn :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.8; }
  .btn-content { display: inline-flex; align-items: center; gap: 8px; }

  .mark { width: 5px; height: 5px; flex-shrink: 0; box-sizing: border-box;
    border: 1px solid currentColor; background: transparent; transform: rotate(45deg); }
  .btn.ghost .mark { width: 6px; height: 1px; border: 0; background: currentColor; transform: none; }

  .btn.primary { border-color: var(--bb-tan); color: var(--bb-tan-light); }
  .btn.primary:not(:disabled):not(.is-done):hover { background: var(--bb-tan); color: var(--bb-black); }
  .btn.ghost { color: var(--bb-muted); --btn-spinner: var(--bb-muted); }
  .btn.ghost:not(:disabled):not(.is-done):hover { color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  /* secondary is the quiet tan outline; `tan` kept as an identical alias. */
  .btn.secondary, .btn.tan, .btn.icon { border-color: rgba(201,168,124,0.28); color: var(--bb-tan-light); }
  .btn.secondary:not(:disabled):not(.is-done):hover,
  .btn.tan:not(:disabled):not(.is-done):hover,
  .btn.icon:not(:disabled):not(.is-done):hover { background: rgba(201,168,124,0.08); border-color: rgba(201,168,124,0.5); color: var(--bb-tan-pale); }
  .btn.destructive { border: 1px dashed rgba(217,138,138,0.4); color: #d98a8a; --btn-spinner: #d98a8a; }
  .btn.destructive:not(:disabled):not(.is-done):hover { background: rgba(217,138,138,0.08); border-color: rgba(217,138,138,0.6); }

  .btn.sm { padding: 7px 12px 7px 10px; font-size: 11.5px; gap: 9px; }
  .btn.sm .mark { width: 4px; height: 4px; }
  .btn.sm.ghost .mark { width: 6px; height: 1px; }

  /* icon-only: a 40px square, no mark, secondary frame. */
  .btn.icon { width: 40px; height: 40px; padding: 0; justify-content: center; }

  /* done: the resting "saved" state. Mark goes solid green, hover is inert. */
  .btn.is-done { border: 1px solid rgba(82,183,136,0.4); background: rgba(82,183,136,0.07);
    color: var(--bb-green-glow); --btn-spinner: var(--bb-green-glow); }
  .btn.is-done .mark { background: currentColor; border: 0; }

  .btn:disabled { opacity: 0.4; cursor: not-allowed; }

  /* Loading hides the label with `color: transparent`, so the button keeps its
     exact width and the mark collapses to an invisible spacer, and centers a
     spinner over it. aria-busy carries the state to AT. */
  .btn.is-loading { color: transparent; cursor: progress; }
  .spinner {
    position: absolute;
    top: 50%; left: 50%;
    width: 14px; height: 14px;
    margin: -7px 0 0 -7px;
    border: 2px solid var(--btn-spinner);
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
