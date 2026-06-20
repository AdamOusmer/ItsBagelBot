<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    href,
    icon,
    label,
    active = false,
    locked = false,
    count
  }: {
    href: string;
    icon: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    count?: string | number;
  } = $props();
</script>

{#if locked}
  <span class="nav-item locked" title="Broadcaster only">
    <Icon name={icon} /> {label}
    <Icon name="lock" size={13} />
  </span>
{:else}
  <a class="nav-item {active ? 'active' : ''}" {href} aria-current={active ? 'page' : undefined}>
    <Icon name={icon} /> {label}
    {#if count !== undefined}<span class="count">{count}</span>{/if}
  </a>
{/if}

<style>
  .nav-item {
    display: flex; align-items: center; gap: 12px; padding: 10px 12px;
    border-radius: var(--bb-radius-md); cursor: pointer; position: relative;
    color: var(--bb-muted); border: 1px solid transparent;
    transition: color var(--bb-dur-base) var(--bb-ease-out-expo),
                background var(--bb-dur-base) var(--bb-ease-out-expo),
                border-color var(--bb-dur-base) var(--bb-ease-out-expo);
    font-family: var(--bb-font-body); font-size: 14px; font-weight: 500;
    background: none; width: 100%; text-align: left; text-decoration: none;
  }
  .nav-item :global(svg) { width: 18px; height: 18px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; flex-shrink: 0; }
  .nav-item:hover { color: var(--bb-white); background: rgba(255,255,255,0.03); }
  .nav-item.active { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--glass-border); }
  .nav-item.active::before {
    content: ""; position: absolute; left: -16px; top: 50%; transform: translateY(-50%);
    width: 3px; height: 20px; border-radius: 0 3px 3px 0;
    background: var(--ui-accent-glow); box-shadow: 0 0 12px var(--ui-accent-glow);
  }
  .count { margin-left: auto; font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); }
  .nav-item.locked { opacity: 0.45; cursor: not-allowed; }
</style>
