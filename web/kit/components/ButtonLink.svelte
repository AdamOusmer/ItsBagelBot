<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // A link that LOOKS like a Button, for navigation. Real <a href> so it gets
  // native link semantics (open in new tab, right-click, screen-reader "link"
  // role). Never a <button> faking navigation. Visual variants mirror Button.
  import type { Snippet } from 'svelte';
  let {
    href,
    variant = 'ghost',
    size = 'md',
    done = false,
    class: cls = '',
    children,
    ...rest
  }: {
    href: string;
    variant?: 'primary' | 'secondary' | 'ghost' | 'destructive' | 'icon' | 'tan';
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
</script>

<a
  class="btn {variant} {cls}"
  class:sm={size === 'sm'}
  class:is-done={done}
  {href}
  {...rest}
  data-mark=""
>
  {#if variant !== 'icon'}<i class="mark" aria-hidden="true"></i>{/if}
  {#if children}{@render children()}{/if}
</a>

<style>
  /* Kept in step with Button.svelte's scoped styles so a link and a button read
     as the same control. */
  .btn { font-family: var(--bb-font-mono); font-size: 12.5px; letter-spacing: 0.04em; line-height: 1;
    padding: 13px 20px 13px 16px; border-radius: var(--bb-radius-sm); cursor: pointer; white-space: nowrap; position: relative;
    text-decoration: none; background: transparent; border: 1px solid transparent;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); display: inline-flex; align-items: center; gap: 12px; }
  .btn :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.8; }

  .mark { width: 5px; height: 5px; flex-shrink: 0; box-sizing: border-box;
    border: 1px solid currentColor; background: transparent; transform: rotate(45deg); }
  .btn.ghost .mark { width: 6px; height: 1px; border: 0; background: currentColor; transform: none; }

  .btn.primary { border-color: var(--bb-tan); color: var(--bb-tan-light); }
  .btn.primary:not(.is-done):hover { background: var(--bb-tan); color: var(--bb-black); }
  .btn.ghost { color: var(--bb-muted); }
  .btn.ghost:not(.is-done):hover { color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  .btn.secondary, .btn.tan, .btn.icon { border-color: rgba(201,168,124,0.28); color: var(--bb-tan-light); }
  .btn.secondary:not(.is-done):hover,
  .btn.tan:not(.is-done):hover,
  .btn.icon:not(.is-done):hover { background: rgba(201,168,124,0.08); border-color: rgba(201,168,124,0.5); color: var(--bb-tan-pale); }
  .btn.destructive { border: 1px dashed rgba(217,138,138,0.4); color: #d98a8a; }
  .btn.destructive:not(.is-done):hover { background: rgba(217,138,138,0.08); border-color: rgba(217,138,138,0.6); }

  .btn.sm { padding: 7px 12px 7px 10px; font-size: 11.5px; gap: 9px; }
  .btn.sm .mark { width: 4px; height: 4px; }
  .btn.sm.ghost .mark { width: 6px; height: 1px; }

  .btn.icon { width: 40px; height: 40px; padding: 0; justify-content: center; }

  .btn.is-done { border: 1px solid rgba(82,183,136,0.4); background: rgba(82,183,136,0.07); color: var(--bb-green-glow); }
  .btn.is-done .mark { background: currentColor; border: 0; }
</style>
