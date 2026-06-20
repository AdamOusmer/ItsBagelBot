<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  let { root, crumb, actions }: { root: string; crumb: string; actions?: Snippet } = $props();
</script>

<header class="topbar">
  <div class="crumb">
    <span>{root}</span><span class="sep">/</span><span class="here">{crumb}</span>
  </div>
  <div class="grow"></div>
  {#if actions}
    {@render actions()}
  {:else}
    <button class="icon-btn" aria-label="Notifications"><Icon name="bell" size={17} /></button>
  {/if}
</header>

<style>
  .topbar {
    position: sticky; top: 0; z-index: 40;
    display: flex; align-items: center; gap: 10px;
    padding: 14px 16px;
    background: rgba(10,10,10,0.5);
    border-bottom: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright));
    -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat)) brightness(var(--glass-bright));
  }
  @media (min-width: 761px) {
    .topbar { gap: 18px; padding: 16px var(--gutter); }
  }
  .crumb { font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--bb-muted); display: flex; align-items: center; gap: 8px; }
  .crumb .sep { opacity: 0.5; }
  .crumb .here { color: var(--bb-tan); }
  .grow { flex: 1; }
  .icon-btn { width: 40px; height: 40px; border-radius: var(--bb-radius-pill); display: flex; align-items: center; justify-content: center;
    background: rgba(255,255,255,0.03); border: 1px solid var(--glass-border); color: var(--bb-tan-light); cursor: pointer;
    transition: all var(--bb-dur-base) var(--bb-ease-out-back); }
  .icon-btn :global(svg) { width: 17px; height: 17px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .icon-btn:hover { background: rgba(201,168,124,0.08); color: var(--bb-tan-pale); }
</style>
