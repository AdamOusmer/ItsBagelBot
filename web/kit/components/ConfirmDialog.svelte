<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
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

<Modal {open} {title} {busy} closeModal={onCancel}>
  {#if body}<p class="modal-body">{body}</p>{/if}
  {#if children}{@render children()}{/if}
  <div class="modal-actions">
    <button type="button" class="bb-btn bb-btn--ghost" onclick={onCancel} disabled={busy}>{cancelLabel}</button>
    <button
      type="button"
      class="bb-btn {danger ? 'confirm-danger' : 'bb-btn--primary'}"
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
  /* `color` is re-declared here on purpose: a bare `.bb-btn` is the primary
     now, and the contract's primary hover paints the label --bb-black. Before
     the button contract moved into @bagel/ui a bare `.btn` had no hover rule,
     so this only had to name the two things that changed. */
  .confirm-danger:hover {
    background: rgba(176, 90, 70, 0.28);
    box-shadow: 0 0 18px rgba(176, 90, 70, 0.25);
    color: #cf8a78;
  }
</style>
