<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { InspectorStatus } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    LEVELS,
    LEVEL_LABEL,
    SCOPE_LABEL,
    type ComposeDraft,
    type NotificationLevel,
    type NotificationScope
  } from './notification-compose';

  let {
    draft = $bindable(),
    status,
    dirty,
    canSave,
    onCancel,
    onSubmit
  }: {
    draft: ComposeDraft;
    status: InspectorStatus;
    dirty: boolean;
    canSave: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  const scopeOptions = $derived(
    (['broadcast', 'direct'] as const).map((s) => ({ value: s, label: t(SCOPE_LABEL[s]) }))
  );
</script>

<form class="editor" method="POST" action="?/send" use:enhance={onSubmit}>
  <Scroller fill padding="18px" smooth>
    <Stack gap={1}>
      <section class="block">
        <Heading level={3} variant="label">{t('admin.notifications.audienceLabel')}</Heading>
        <RadioGroup
          name="scope"
          options={scopeOptions}
          label={t('admin.notifications.audienceLabel')}
          bind:value={
            () => draft.scope,
            (v) => (draft = { ...draft, scope: v as NotificationScope })
          }
        />
      </section>

      {#if draft.scope === 'direct'}
        <Field label={t('admin.notifications.fieldUserId')}>
          <Input
            fill mono
            type="text"
            name="target_user_id"
            inputmode="numeric"
            autocomplete="off"
            placeholder={t('admin.notifications.fieldUserIdPlaceholder')}
            bind:value={draft.targetUserId}
          />
        </Field>
        <Field label={t('admin.notifications.fieldUsername')}>
          <Input
            fill mono
            type="text"
            name="target_username"
            autocomplete="off"
            placeholder={t('admin.notifications.fieldUsernamePlaceholder')}
            bind:value={draft.targetUsername}
          />
        </Field>
      {/if}

      <Field label={t('admin.notifications.fieldTitle')}>
        <Input
          fill mono
          type="text"
          name="title"
          maxlength="120"
          required
          placeholder={t('admin.notifications.fieldTitlePlaceholder')}
          bind:value={draft.title}
        />
      </Field>

      <Field label={t('admin.notifications.fieldBody')}>
        <Textarea
          fill
          name="body"
          maxlength="2000"
          rows={4}
          required
          placeholder={t('admin.notifications.fieldBodyPlaceholder')}
          bind:value={draft.body}
        ></Textarea>
      </Field>

      <Field label={t('admin.notifications.fieldLevel')}>
        <Select
          fill
          name="level"
          options={LEVELS.map((level) => ({ value: level, label: t(LEVEL_LABEL[level as NotificationLevel]) }))}
          bind:value={() => draft.level, (value) => (draft.level = value as NotificationLevel)}
        />
      </Field>

      <Field label={t('admin.notifications.fieldExpires')} tag={t('common.optional')}>
        <Input
          fill mono
          type="datetime-local"
          name="expires_at"
          bind:value={draft.expiresAt}
        />
      </Field>
      <Text size="sm" tone="muted">{t('admin.notifications.expiresHint')}</Text>
    </Stack>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={t('admin.notifications.sendCta')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('admin.notifications.sending')}
    savedLabel={t('admin.notifications.sent')}
    dirtyLabel={t('admin.notifications.readyToSend')}
    errorLabel={t('admin.notifications.sendFailed')}
    {onCancel}
  />
</form>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    margin-bottom: 14px;
  }
</style>
