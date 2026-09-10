<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-row` contract
  // (../styles/elements/management-row.css). Astro twin:
  // ../astro/ManagementRow.astro (the guide screens draw a static row).
  //
  // THE STRUCTURE IS THE ACCESSIBILITY FIX. The row's primary content is a
  // single <button> (announced with aria-expanded / aria-controls); the quick
  // actions are SIBLINGS of that button, never nested inside it. The audit
  // flagged the old rows for putting role="button" on a container that wrapped
  // a switch and a delete button: invalid, and a screen-reader trap. Anything
  // that re-nests them re-creates that bug silently, which is why this note
  // travels with the component rather than living in the CSS alone.
  //
  // No aria-label on the button: its accessible name comes from the visible
  // content of the `primary` snippet (name + response + metadata), so assistive
  // tech announces everything a sighted user sees, not just the title.
  //
  // `row-shell` is emitted alongside the contract class and is NOT a second
  // name for it: it is the console's cross-component last-child marker, also
  // emitted by QuoteRow and ModuleCommandRow, which pages use to strip the
  // final separator (`.list :global(.row-shell:last-child)`). It goes when
  // those two rows move into this package.
  import '../styles/elements/management-row.css';
  import type { Snippet } from 'svelte';

  let {
    selected = false,
    expanded = false,
    controls,
    disabled = false,
    accent = false,
    class: className = '',
    onselect,
    primary,
    actions,
    ...rest
  }: {
    selected?: boolean;
    expanded?: boolean;
    /** id of the region the row's aria-controls points at. */
    controls?: string;
    disabled?: boolean;
    /** Leading edge on the selected row, for a deck that docks an inspector. */
    accent?: boolean;
    class?: string;
    onselect?: () => void;
    primary?: Snippet;
    actions?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-row',
      'row-shell',
      accent ? 'bb-row--accent' : null,
      selected ? 'is-selected' : null,
      disabled ? 'is-off' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}><button
    class="bb-row__primary"
    type="button"
    data-cursor="quiet"
    aria-expanded={expanded}
    aria-controls={controls}
    aria-current={selected ? 'true' : undefined}
    onclick={onselect}
  >{#if primary}{@render primary()}{/if}</button>{#if actions}<div class="bb-row__actions"
    >{@render actions()}</div
  >{/if}</div>
