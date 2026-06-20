<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    variant = 'ghost',
    icon,
    type = 'button',
    onclick,
    class: cls = '',
    children,
    ...rest
  }: {
    variant?: 'primary' | 'ghost' | 'tan';
    icon?: IconName;
    type?: 'button' | 'submit';
    onclick?: () => void;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<button class="btn {variant} {cls}" {type} {onclick} {...rest}>
  {#if icon}<Icon name={icon} size={14} />{/if}
  {@render children()}
</button>

<style>
  .btn { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase;
    padding: 12px 20px; border-radius: var(--bb-radius-pill); cursor: pointer; white-space: nowrap;
    border: 1px solid var(--glass-border); transition: all var(--bb-dur-base) var(--bb-ease-out-back); display: inline-flex; align-items: center; gap: 8px; }
  .btn :global(svg) { width: 14px; height: 14px; stroke: currentColor; fill: none; stroke-width: 1.8; }
  .btn.primary { background: var(--ui-accent); color: var(--bb-white); border-color: var(--ui-accent-light); }
  .btn.primary:hover { background: var(--ui-accent-light); box-shadow: 0 0 24px rgba(82,183,136,0.32); }
  .btn.ghost { background: rgba(255,255,255,0.03); color: var(--bb-tan-light); }
  .btn.ghost:hover { background: rgba(201,168,124,0.08); color: var(--bb-tan-pale); border-color: var(--bb-border-strong); }
  .btn.tan { background: var(--bb-tan); color: #0a0a0a; border-color: var(--bb-tan-light); }
  .btn.tan:hover { background: var(--bb-tan); color: var(--bb-green); border-color: var(--bb-green-light); box-shadow: 0 0 24px rgba(82,183,136,0.3); }
</style>
