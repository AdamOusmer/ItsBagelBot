<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    href,
    icon,
    label,
    active = false,
    locked = false,
    count,
    index
  }: {
    href: string;
    icon: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    count?: string | number;
    index?: number;
  } = $props();

  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : undefined);
</script>

{#if locked}
  <span class="nav-item locked" title="Broadcaster only">
    {#if idx}<span class="idx">{idx}</span>{/if}
    <Icon name={icon} /> {label}
    <Icon name="lock" size={13} />
  </span>
{:else}
  <a class="nav-item {active ? 'active' : ''}" {href} aria-current={active ? 'page' : undefined}>
    {#if idx}<span class="idx">{idx}</span>{/if}
    <Icon name={icon} /> <span class="lbl">{label}</span>
    {#if count !== undefined}<span class="count">{count}</span>{/if}
  </a>
{/if}

<style>
  /* Ledger rail entry: indexed mono line on a hairline, active keyed by a tan
     bracket + index instead of a filled pill. */
  .nav-item {
    display: flex; align-items: center; gap: 10px; padding: 11px 10px 11px 12px;
    cursor: pointer; position: relative;
    color: var(--bb-muted); border: none; border-left: 1px solid transparent;
    transition: color var(--bb-dur-base) var(--bb-ease-out-expo),
                border-color var(--bb-dur-base) var(--bb-ease-out-expo);
    font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.08em; text-transform: uppercase;
    background: none; width: 100%; text-align: left; text-decoration: none;
  }
  .idx { font-size: 10px; color: var(--bb-muted); opacity: 0.6; min-width: 18px; transition: color var(--bb-dur-base) ease, opacity var(--bb-dur-base) ease; }
  .nav-item :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; flex-shrink: 0; }
  .lbl { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .nav-item:hover { color: var(--bb-white); }
  .nav-item:hover .idx { opacity: 1; }
  .nav-item.active { color: var(--bb-white); border-left-color: var(--bb-tan); }
  .nav-item.active .idx { color: var(--bb-tan); opacity: 1; }
  .nav-item.active::after {
    content: ""; position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
    width: 5px; height: 5px; background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .count { margin-left: auto; font-size: 10px; color: var(--bb-muted); }
  .nav-item.locked { opacity: 0.45; cursor: not-allowed; }
</style>
