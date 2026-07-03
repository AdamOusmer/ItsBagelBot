<script lang="ts">
  // A scroll region wearing the console's thin tan scrollbar instead of the
  // browser's default chrome bar. Drop it anywhere a panel needs to scroll:
  // the account menu's shared-dashboards list, the command inspector, and so on.
  // Extra attributes (role, aria-*, data-lenis-prevent, …) pass straight through
  // to the scrolling element.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    maxHeight,
    fill = false,
    padding,
    children,
    ...rest
  }: {
    // Height cap before scrolling kicks in (e.g. '208px'). Omit to grow freely.
    maxHeight?: string;
    // Fill a flex parent and scroll the overflow (flex:1; min-height:0).
    fill?: boolean;
    // Inner padding kept inside the scroll region so the bar hugs the edge.
    padding?: string;
    children: Snippet;
  } & HTMLAttributes<HTMLDivElement> = $props();

  const style = $derived(
    [maxHeight && `max-height:${maxHeight}`, padding && `padding:${padding}`]
      .filter(Boolean)
      .join(';') || undefined
  );
</script>

<div class="scroller" class:fill {style} {...rest}>
  {@render children()}
</div>

<style>
  .scroller {
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: rgba(201, 168, 124, 0.35) transparent;
  }
  .scroller.fill { flex: 1; min-height: 0; }
  .scroller::-webkit-scrollbar { width: 6px; }
  .scroller::-webkit-scrollbar-thumb { background: rgba(201, 168, 124, 0.35); border-radius: 999px; }
</style>
