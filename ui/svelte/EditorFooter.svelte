<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/editor-footer.css';
  import Button from './Button.svelte';

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
    class: className = '',
    onCancel,
    ...rest
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
    class?: string;
    onCancel: () => void;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-editor-foot', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}><span
    class="bb-editor-foot__status"
    role="status"
    aria-live="polite"
  >{#if status === 'saving'}<span class="bb-editor-foot__s bb-editor-foot__s--saving"
      >{savingLabel}</span
    >{:else if status === 'error' || status === 'conflict'}<span
      class="bb-editor-foot__s bb-editor-foot__s--error">{errorLabel}</span
    >{:else if status === 'saved'}<span class="bb-editor-foot__s bb-editor-foot__s--saved"
      >{savedLabel}</span
    >{:else if dirty}<span class="bb-editor-foot__s bb-editor-foot__s--dirty">{dirtyLabel}</span
    >{/if}</span
  ><span class="bb-editor-foot__acts"><Button variant="ghost" onclick={onCancel}
      >{cancelLabel}</Button
    ><Button type="submit" disabled={!canSave || status === 'saving'}
      >{status === 'saving' ? savingLabel : saveLabel}</Button
    ></span
  ></div>
