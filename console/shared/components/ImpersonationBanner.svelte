<script lang="ts">
  import type { Snippet } from 'svelte';
  let { exitHref, exitForm = false, children }:
    { exitHref?: string; exitForm?: boolean; children: Snippet } = $props();
</script>

<div class="imp-banner" role="status">
  <span>{@render children()}</span>
  {#if exitForm}
    <form method="POST" action="/auth/logout"><button type="submit" class="imp-exit">Exit</button></form>
  {:else}
    <a href={exitHref} class="imp-exit">Exit</a>
  {/if}
</div>

<style>
  .imp-banner {
    position: fixed; top: 0; left: 0; right: 0; z-index: 300;
    display: flex; align-items: center; justify-content: center; gap: 1rem;
    padding: 8px 14px;
    font-family: var(--bb-font-body); font-size: 13px;
    color: var(--bb-bg-1, #111);
    background: var(--bb-tan, #c9a87c);
    border-bottom: 1px solid rgba(0, 0, 0, 0.2);
  }
  .imp-banner :global(b) { font-weight: 700; }
  .imp-banner form { display: inline; }
  .imp-exit {
    cursor: pointer; font: inherit; font-weight: 700;
    display: inline-block; text-decoration: none; line-height: 1.4;
    padding: 3px 12px; border-radius: var(--bb-radius-sm, 8px);
    color: var(--bb-bg-1, #111);
    background: transparent; border: 1px solid rgba(0, 0, 0, 0.35);
  }
  .imp-exit:hover { background: rgba(0, 0, 0, 0.12); }
</style>
