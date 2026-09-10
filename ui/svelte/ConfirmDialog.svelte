<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Confirmation preset over ./Modal.svelte, which owns Escape, the backdrop
  // and the focus trap. No Astro twin and no contract file of its own: it is
  // `.bb-modal` plus `.bb-modal__body` and `.bb-modal__actions`, and an element
  // whose CSS is entirely another element's is a composition, not a new one.
  //
  // USE IT FOR DESTRUCTIVE OR HARD-TO-REVERSE ACTIONS ONLY. For anything cheap
  // to restore, apply optimistically and offer an Undo toast: a confirmation
  // dialog on a reversible action trains people to dismiss confirmations, which
  // is what makes the irreversible one dangerous.
  //
  // The action buttons are ./Button.svelte, the sibling adapter. The literal
  // `.btn ghost` / `.btn primary` strings this used to write are gone, and so
  // is the `confirm-danger` tone modal.css carried for them: `danger` selects
  // the contract's `destructive` variant, which is DASHED rather than tinted
  // on purpose (button.css records why: colour alone is not a warning for the
  // ~8% of men with a red-green deficiency).
  import '../styles/elements/modal.css';
  import type { Snippet } from 'svelte';
  import Button from './Button.svelte';
  import Modal from './Modal.svelte';

  let {
    open = false,
    title,
    body = undefined as string | undefined,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    busyLabel = 'Working…',
    danger = false,
    busy = false,
    onConfirm,
    onCancel,
    children = undefined as Snippet | undefined,
  }: {
    open: boolean;
    title: string;
    body?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    busyLabel?: string;
    danger?: boolean;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
    children?: Snippet;
  } = $props();
</script>

<Modal {open} {title} {busy} closeModal={onCancel}>
  {#if body}<p class="bb-modal__body">{body}</p>{/if}
  {#if children}{@render children()}{/if}
  <div class="bb-modal__actions">
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{cancelLabel}</Button>
    <Button
      variant={danger ? 'destructive' : 'primary'}
      onclick={onConfirm}
      disabled={busy}
    >
      {busy ? busyLabel : confirmLabel}
    </Button>
  </div>
</Modal>
