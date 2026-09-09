<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Direct notification composer. It posts to the NOTIFICATIONS route's send
  // action, not to one of this page's own: delivery, audit and idempotency stay
  // in one place, and a second copy of that write is exactly what would drift.
  //
  // It owns its three fields because nothing else on the page reads them, and
  // it resets them on each open so a cancelled draft never reappears attached to
  // the next user.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import ConfirmDialog from '@bagel/shared/components/ConfirmDialog.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';

  let {
    open = $bindable(),
    login,
    userId,
    busy,
    onSubmit
  }: {
    open: boolean;
    login: string;
    userId: string;
    busy: boolean;
    /** Runs the enhance callback; the caller closes the dialog from it. */
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  const LEVELS = [
    { value: 'info', label: 'admin.users.messageLevelInfo' },
    { value: 'success', label: 'admin.users.messageLevelSuccess' },
    { value: 'warning', label: 'admin.users.messageLevelWarning' },
    { value: 'critical', label: 'admin.users.messageLevelCritical' }
  ] as const;

  let title = $state('');
  let body = $state('');
  let level = $state('info');
  let form = $state<HTMLFormElement | null>(null);

  // Clear on each open rather than on close: a close can happen mid-flight, and
  // wiping the fields under an in-flight submit is how a blank notification got
  // sent once.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      title = '';
      body = '';
      level = 'info';
    }
    wasOpen = open;
  });
</script>

<ConfirmDialog
  {open}
  title={t('admin.users.messageTitle', { login })}
  body={t('admin.users.messageBody')}
  confirmLabel={t('admin.users.messageSend')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (open = false)}
  onConfirm={() => form?.requestSubmit()}
>
  <div class="fields">
    <label class="field">
      {t('admin.users.messageFieldTitle')}
      <input class="text-input" type="text" maxlength="120" bind:value={title} />
    </label>
    <label class="field">
      {t('admin.users.messageFieldBody')}
      <textarea class="text-input" rows="3" maxlength="2000" bind:value={body}></textarea>
    </label>
    <label class="field">
      {t('admin.users.messageFieldLevel')}
      <select class="text-input" bind:value={level}>
        {#each LEVELS as lvl (lvl.value)}
          <option value={lvl.value}>{t(lvl.label)}</option>
        {/each}
      </select>
    </label>
  </div>
</ConfirmDialog>

<form method="POST" action="/notifications?/send" use:enhance={onSubmit} bind:this={form} hidden>
  <input type="hidden" name="scope" value="direct" />
  <input type="hidden" name="target_user_id" value={userId} />
  <input type="hidden" name="target_username" value="" />
  <input type="hidden" name="title" value={title} />
  <input type="hidden" name="body" value={body} />
  <input type="hidden" name="level" value={level} />
  <input type="hidden" name="expires_at" value="" />
</form>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 12px 0 4px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .text-input {
    padding: 8px 11px;
    font-family: var(--bb-font-mono);
    font-size: 12.5px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-1, #16130f);
    color: var(--bb-white);
  }
  .text-input:focus {
    outline: none;
    border-color: var(--bb-border-strong);
  }
  textarea.text-input {
    resize: vertical;
    font-family: var(--bb-font-body);
  }
</style>
