<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Labeled toggle row: a text column (label + optional description) plus an
  // owned Switch, with an optional hidden input for a server-posted form.
  // Replaces the copy-pasted `.setrow` block that lived in songqueue and
  // govee's reward editor — two near-identical forks, one of which (songqueue)
  // was missing the `.setrow.warn` rule the other had, so its "live only"
  // warn state silently did nothing.
  //
  // The hidden input's on/off strings are settable because the stored flag is
  // sometimes the inverse of the switch (e.g. a "live only" switch posting an
  // `allow_offline` field): pass onValue/offValue to encode that at the call
  // site instead of inverting `checked` itself.
  import Switch from './Switch.svelte';

  let {
    label,
    description,
    warn = false,
    checked = $bindable(false),
    disabled = false,
    pending = false,
    onchange,
    name,
    onValue = 'on',
    offValue = ''
  }: {
    label: string;
    // Muted helper line under the label, e.g. "Requests are open." Also wired
    // as the switch's aria-describedby when present.
    description?: string;
    // Flags the row's label amber (matches the govee original) — the caller
    // decides the condition, since it isn't always simply "off".
    warn?: boolean;
    checked?: boolean;
    disabled?: boolean;
    pending?: boolean;
    onchange?: (v: boolean) => void;
    // When set, renders a hidden input alongside the switch for a plain form
    // POST. Omit for switches that only drive local state.
    name?: string;
    onValue?: string;
    offValue?: string;
  } = $props();

  const uid = $props.id();
  const descId = `setrow-desc-${uid}`;
</script>

<div class="setrow" class:on={checked} class:warn>
  <div class="setrow-text">
    <span class="setrow-label">{label}</span>
    {#if description}<span class="setrow-desc" id={descId}>{description}</span>{/if}
  </div>
  <Switch bind:checked {disabled} {pending} {onchange} {label} describedby={description ? descId : undefined} />
</div>
{#if name}<input type="hidden" {name} value={checked ? onValue : offValue} />{/if}

<style>
  .setrow {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 11px 12px;
    border: 1px solid var(--rule);
    border-radius: 8px;
    margin-bottom: 14px;
  }
  .setrow.on { border-color: var(--rule-tan); background: rgba(201, 168, 124, 0.06); }
  /* Text wraps at any width instead of overflowing the row; the switch keeps
     its own size and never gets squeezed by long labels on a 320px screen. */
  .setrow-text { display: grid; gap: 2px; flex: 1; min-width: 0; }
  .setrow-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .setrow.warn .setrow-label { color: #d9a441; }
  .setrow-desc { margin: 0; font-family: var(--bb-font-body); font-size: 12px; line-height: 1.5; color: var(--bb-muted); }
  /* Nudge the switch down so its pill lines up with the label's cap-height
     instead of the vertical center of a two-line description. */
  .setrow :global(.switch) { flex-shrink: 0; margin-top: 1px; }
</style>
