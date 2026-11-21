<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
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
  const levelOptions = $derived(LEVELS.map(({ value, label }) => ({ value, label: t(label) })));

  let title = $state('');
  let body = $state('');
  let level = $state('info');
  let form = $state<HTMLFormElement>();

  $effect(() => {
    if (open) return;
    title = '';
    body = '';
    level = 'info';
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
    <Stack gap={3}>
      <Field fit label={t('admin.users.messageFieldTitle')}>
        <Input fill maxlength={120} bind:value={title} />
      </Field>
      <Field fit label={t('admin.users.messageFieldBody')}>
        <Textarea fill rows={3} maxlength={2000} bind:value={body} />
      </Field>
      <Field fit label={t('admin.users.messageFieldLevel')}>
        <Select fill label={t('admin.users.messageFieldLevel')} options={levelOptions} bind:value={level} />
      </Field>
    </Stack>
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
    margin: var(--bb-space-3) 0 var(--bb-space-1);
  }
</style>
