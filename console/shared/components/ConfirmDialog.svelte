<script lang="ts">
  // Confirmation wrapper over the shared Modal (which owns Escape + backdrop
  // close). Use for destructive or hard-to-reverse actions; prefer optimistic
  // apply + undo toast for cheap-to-restore ones.
  import type { Snippet } from 'svelte';
  import Modal from './Modal.svelte';

  let {
    open = false,
    title,
    body = undefined as string | undefined,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    danger = false,
    busy = false,
    onConfirm,
    onCancel,
    children = undefined as Snippet | undefined
  }: {
    open: boolean;
    title: string;
    body?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
    children?: Snippet;
  } = $props();
</script>

<Modal {open} {title} closeModal={onCancel}>
  {#if body}<p class="modal-body">{body}</p>{/if}
  {#if children}{@render children()}{/if}
  <div class="modal-actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{cancelLabel}</button>
    <button
      type="button"
      class="btn {danger ? 'confirm-danger' : 'primary'}"
      onclick={onConfirm}
      disabled={busy}
    >
      {busy ? 'Working…' : confirmLabel}
    </button>
  </div>
</Modal>

<style>
  .confirm-danger {
    background: rgba(176, 90, 70, 0.15);
    border-color: rgba(176, 90, 70, 0.4);
    color: #cf8a78;
  }
  .confirm-danger:hover {
    background: rgba(176, 90, 70, 0.28);
    box-shadow: 0 0 18px rgba(176, 90, 70, 0.25);
  }
</style>
