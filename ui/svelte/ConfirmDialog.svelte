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
    open = $bindable(false),
    title,
    body = undefined as string | undefined,
    confirmLabel = i18n.t('action.confirm'),
    cancelLabel = i18n.t('action.cancel'),
    busyLabel = i18n.t('status.working'),
    tone = 'neutral',
    busy = false,
    onConfirm,
    onCancel,
    onOpenChange,
    children = undefined as Snippet | undefined,
  }: {
    open: boolean;
    title: string;
    body?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    busyLabel?: string;
    tone?: 'neutral' | 'danger';
    busy?: boolean;
    onConfirm: () => void;
    onCancel?: () => void;
    onOpenChange?: (open: boolean) => void;
    children?: Snippet;
  } = $props();

  const uid = $props.id();
  const bodyId = `bb-confirm-body-${uid}`;

  function cancel() {
    open = false;
    onOpenChange?.(false);
    onCancel?.();
  }
</script>

<Modal
  {open}
  {title}
  {busy}
  onClose={cancel}
  role={tone === 'danger' ? 'alertdialog' : 'dialog'}
  describedBy={body ? bodyId : undefined}
>
  {#if body}<p class="bb-modal__body" id={bodyId}>{body}</p>{/if}
  {#if children}{@render children()}{/if}
  <div class="bb-modal__actions">
    <Button variant="ghost" onclick={cancel} disabled={busy}>{cancelLabel}</Button>
    <Button
      {tone}
      onclick={onConfirm}
      disabled={busy}
    >
      {busy ? busyLabel : confirmLabel}
    </Button>
  </div>
</Modal>
