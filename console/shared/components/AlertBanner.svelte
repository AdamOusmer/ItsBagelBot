<script lang="ts">
  // Inline status/degraded banner. `danger` reproduces the old page-local
  // .degraded look; `warn` is the tan variant. Message goes in the default
  // slot; an optional action snippet trails on the right.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  let {
    variant = 'danger',
    icon = 'ban',
    children,
    action
  }: {
    variant?: 'danger' | 'warn';
    icon?: IconName;
    children: Snippet;
    action?: Snippet;
  } = $props();
</script>

<div class="alert {variant}" role="alert">
  <Icon name={icon} size={13} />
  <span class="msg">{@render children()}</span>
  {#if action}{@render action()}{/if}
</div>

<style>
  .alert {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border-radius: 8px;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
  .alert.danger {
    border: 1px solid rgba(176, 90, 70, 0.4);
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
  }
  .alert.warn {
    border: 1px solid rgba(201, 168, 124, 0.4);
    background: rgba(201, 168, 124, 0.08);
    color: var(--bb-tan-light);
  }
  .msg { flex: 1; }
</style>
