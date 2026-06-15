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
