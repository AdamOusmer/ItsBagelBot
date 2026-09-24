<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
