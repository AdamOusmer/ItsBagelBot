<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getUiI18n } from './i18n';

  import '../styles/elements/modal.css';
  import type { Snippet } from 'svelte';
  import Button from './Button.svelte';
  import Modal from './Modal.svelte';

  const i18n = getUiI18n();
  let {
    open = false,
    title,
    body = undefined as string | undefined,
    confirmLabel = i18n.t('action.confirm'),
    cancelLabel = i18n.t('action.cancel'),
    busyLabel = i18n.t('status.working'),
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
