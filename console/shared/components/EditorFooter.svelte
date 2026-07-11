<script lang="ts">
  // Sticky action footer for an inspector editor. Keeps Save/Cancel and the save
  // status visible below a long scrolling form (the audit flagged actions that
  // sat below the fold). Meant to live as a sibling *after* the scroll area so it
  // never scrolls out of view. The consumer wraps its fields + this footer in a
  // <form use:enhance>; Save is the form's submit button.
  import Icon from './Icon.svelte';

  let {
    status = 'idle',
    dirty = false,
    canSave = true,
    saveLabel = 'Save',
    cancelLabel = 'Cancel',
    savingLabel = 'Saving…',
    savedLabel = 'Saved',
    errorLabel = 'Could not save',
    dirtyLabel = 'Unsaved changes',
    onCancel
  }: {
    status?: 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
    dirty?: boolean;
    canSave?: boolean;
    saveLabel?: string;
    cancelLabel?: string;
    savingLabel?: string;
    savedLabel?: string;
    errorLabel?: string;
    dirtyLabel?: string;
    onCancel: () => void;
  } = $props();
</script>

<div class="editor-footer">
  <span class="status" role="status" aria-live="polite">
    {#if status === 'saving'}
      <span class="s saving">{savingLabel}</span>
    {:else if status === 'error' || status === 'conflict'}
      <span class="s error">{errorLabel}</span>
    {:else if status === 'saved'}
      <span class="s saved"><Icon name="check" size={12} /> {savedLabel}</span>
    {:else if dirty}
      <span class="s dirty">{dirtyLabel}</span>
    {/if}
  </span>
  <span class="acts">
    <button type="button" class="btn ghost" onclick={onCancel}>{cancelLabel}</button>
    <button type="submit" class="btn primary" disabled={!canSave || status === 'saving'}>
      <Icon name="check" size={14} />
      {status === 'saving' ? savingLabel : saveLabel}
    </button>
  </span>
</div>

<style>
  .editor-footer {
    position: sticky;
    bottom: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
    border-top: 1px solid var(--rule);
    background: var(--bb-bg-1, #111);
  }
  .status { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .s {
    font-family: var(--bb-font-body);
    font-size: 12px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .s.dirty { color: var(--bb-muted); }
  .s.saving { color: var(--bb-tan-light, #c8a050); }
  .s.saved { color: var(--bb-green-glow, #52b788); }
  .s.saved :global(svg) { stroke: currentColor; fill: none; stroke-width: 2.4; }
  .s.error { color: #cf8a78; }
  .acts { display: inline-flex; gap: 10px; flex: none; }

  @media (max-width: 480px) {
    .acts .btn { min-height: 44px; }
  }
</style>
