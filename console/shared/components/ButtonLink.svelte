<script lang="ts">
  // A link that LOOKS like a Button, for navigation. Real <a href> so it gets
  // native link semantics (open in new tab, right-click, screen-reader "link"
  // role) — never a <button> faking navigation. Visual variants mirror Button.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    href,
    variant = 'ghost',
    icon,
    class: cls = '',
    children,
    ...rest
  }: {
    href: string;
    variant?: 'primary' | 'secondary' | 'ghost' | 'destructive' | 'icon' | 'tan';
    icon?: IconName;
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

<a class="btn {variant} {cls}" {href} {...rest}>
  {#if icon}<Icon name={icon} size={14} />{/if}
  {#if children}{@render children()}{/if}
</a>

<style>
  /* Kept in step with Button.svelte's scoped styles so a link and a button read
     as the same control. */
  .btn { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase;
    padding: 12px 20px; border-radius: var(--bb-radius-pill); cursor: pointer; white-space: nowrap; position: relative;
    text-decoration: none;
    border: 1px solid var(--glass-border); transition: all var(--bb-dur-base) var(--bb-ease-out-back); display: inline-flex; align-items: center; justify-content: center; gap: 8px; }
  .btn :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.8; }

  .btn.primary { background: var(--ui-accent); color: var(--bb-white); border-color: var(--ui-accent-light); }
  .btn.primary:hover { background: var(--ui-accent-light); box-shadow: 0 0 24px rgba(82,183,136,0.32); }
  .btn.ghost { background: rgba(255,255,255,0.03); color: var(--bb-tan-light); }
  .btn.ghost:hover { background: rgba(201,168,124,0.08); color: var(--bb-tan-pale); border-color: var(--bb-border-strong); }
  .btn.secondary, .btn.tan { background: var(--bb-tan); color: #0a0a0a; border-color: var(--bb-tan-light); }
  .btn.secondary:hover, .btn.tan:hover { background: var(--bb-tan); color: var(--bb-green); border-color: var(--bb-green-light); box-shadow: 0 0 24px rgba(82,183,136,0.3); }
  .btn.destructive { background: var(--bb-status-error-bg, #2a1310); color: var(--bb-status-error-fg, #f0b0a4); border-color: var(--bb-status-error-border, #b05a46); }
  .btn.destructive:hover { background: #3a1a15; box-shadow: 0 0 18px rgba(176, 90, 70, 0.28); }
  .btn.icon { padding: 12px; min-width: 44px; min-height: 44px; }
</style>
