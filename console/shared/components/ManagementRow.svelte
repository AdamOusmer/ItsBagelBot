<script lang="ts">
  // A selectable list row for the management decks. The row's primary content is
  // a single <button> (announced with aria-expanded / aria-controls); the quick
  // actions (toggle, delete) are SIBLINGS of that button, never nested inside it.
  // The audit flagged the old rows for putting role="button" on a container that
  // wrapped a switch and a delete button — invalid, and a screen-reader trap.
  import type { Snippet } from 'svelte';

  let {
    selected = false,
    expanded = false,
    controls,
    disabled = false,
    onselect,
    primary,
    actions
  }: {
    selected?: boolean;
    expanded?: boolean;
    controls?: string;
    disabled?: boolean;
    onselect: () => void;
    primary: Snippet;
    actions?: Snippet;
  } = $props();
</script>

<!-- No aria-label on the button: its accessible name comes from the visible
     content of the `primary` snippet (name + response + metadata), so assistive
     tech announces everything a sighted user sees, not just the title. -->
<div class="mrow row-shell" class:selected class:off={disabled}>
  <button
    class="mrow-primary"
    type="button"
    aria-expanded={expanded}
    aria-controls={controls}
    aria-current={selected ? 'true' : undefined}
    onclick={onselect}
  >
    {@render primary()}
  </button>
  {#if actions}
    <div class="mrow-actions">{@render actions()}</div>
  {/if}
</div>

<style>
  .mrow {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .mrow.selected { background: rgba(201, 168, 124, 0.05); }
  .mrow.off .mrow-primary { opacity: 0.5; }

  .mrow-primary {
    flex: 1 1 auto;
    min-width: 0;
    display: block;
    padding: 12px 14px;
    background: none;
    border: 0;
    margin: 0;
    text-align: left;
    color: inherit;
    font: inherit;
    cursor: pointer;
    user-select: none;
  }
  .mrow-primary:hover { background: rgba(201, 168, 124, 0.045); }
  .mrow-primary:focus-visible {
    outline: 2px solid var(--bb-tan, #c9a87c);
    outline-offset: -2px;
  }

  .mrow-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 14px;
    flex: none;
  }
</style>
