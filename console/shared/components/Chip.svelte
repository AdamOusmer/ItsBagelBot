<script lang="ts">
  import type { Snippet } from 'svelte';
  let { on = false, onclick, class: cls = '', children, ...rest }:
    { on?: boolean; onclick?: () => void; class?: string; children: Snippet; [key: string]: unknown } = $props();
</script>

<button class="chip {on ? 'on' : ''} {cls}" {onclick} {...rest}>{@render children()}</button>

<style>
  .chip { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em; padding: 8px 14px; border-radius: var(--bb-radius-pill); white-space: nowrap;
    background: rgba(255,255,255,0.03); border: 1px solid var(--glass-border); color: var(--bb-muted); cursor: pointer; transition: all var(--bb-dur-base) var(--bb-ease-out-expo); }
  .chip:hover { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }
</style>
