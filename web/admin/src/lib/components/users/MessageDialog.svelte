<script lang="ts">
  import Input from '@bagel/ui/svelte/Input.svelte';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

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
    <Field label={t('admin.users.messageFieldTitle')}>
      <Input fill mono type="text" maxlength="120" bind:value={title} />
    </Field>
    <Field label={t('admin.users.messageFieldBody')}>
      <Textarea rows={3} maxlength={2000} fill mono bind:value={body} />
    </Field>
    <Field label={t('admin.users.messageFieldLevel')}>
      <Select fill bind:value={level}>
        {#each LEVELS as lvl (lvl.value)}
          <option value={lvl.value}>{t(lvl.label)}</option>
        {/each}
      </Select>
    </Field>
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
    --field-mb: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 12px 0 4px;
  }
</style>
