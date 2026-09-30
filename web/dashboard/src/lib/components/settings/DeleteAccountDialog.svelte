<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { matchesLogin } from './confirm-login';

  const { t } = getI18n();

  let {
    open,
    login,
    busy = false,
    onConfirm,
    onCancel
  }: {
    open: boolean;
    login: string;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  } = $props();

  let typed = $state('');
  const matches = $derived(matchesLogin(typed, login));

  $effect(() => {
    if (!open) typed = '';
  });
</script>

<Modal {open} title={t('settings.deleteTitle')} {busy} onClose={onCancel}>
  <p class="bb-modal__body">{t('settings.deleteBody')}</p>
  <Field label={t('settings.deleteConfirmLabel', { login })} hint={t('settings.deleteConfirmHint', { login })}>
    <Input fill mono name="confirm_login" bind:value={typed} autocomplete="off" autocapitalize="off" spellcheck="false" disabled={busy} />
  </Field>
  <div class="bb-modal__actions">
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button onclick={onConfirm} disabled={busy || !matches} tone="danger">
      {busy ? t('settings.working') : t('settings.deleteAccount')}
    </Button>
  </div>
</Modal>
