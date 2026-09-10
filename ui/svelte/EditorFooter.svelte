<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-editor-foot` contract
  // (../styles/elements/editor-footer.css). Astro twin:
  // ../astro/EditorFooter.astro (the guide screens draw a static footer).
  //
  // The sticky action bar under an inspector's editor form. Keeps Save/Cancel
  // and the save status visible below a long scrolling form — the audit flagged
  // actions that sat below the fold. It must live as a sibling AFTER the scroll
  // area, never inside it, or it scrolls away with the form and the fix is
  // undone.
  //
  // The consumer wraps its fields and this footer in a <form use:enhance>; Save
  // is that form's submit button, which is why this component takes no onSave.
  //
  // EVERY LABEL IS A PROP. The console localises; this package holds no copy.
  //
  // The two actions are ./Button.svelte, the sibling adapter — not the literal
  // `.btn ghost` / `.btn primary` strings this carried while the button
  // contract still lived in the console. Cancel is `ghost`, Save is the
  // default `primary` with type="submit", which is what makes it the enclosing
  // <form use:enhance>'s submit button.
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
