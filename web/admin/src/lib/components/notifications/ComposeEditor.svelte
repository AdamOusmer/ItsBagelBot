<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import type { InspectorStatus } from '@bagel/ui/lib/inspector-machine';
  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    LEVEL_LABEL,
    LEVELS,
    MAX_BODY_LENGTH,
    MAX_TITLE_LENGTH,
    SCOPE_LABEL,
    type ComposeDraft,
    type NotificationLevel,
    type NotificationScope
  } from './notification-compose';

  let { draft, status, dirty, canSave, onChange, onCancel, onSubmit }: {
    draft: ComposeDraft;
    status: InspectorStatus;
    dirty: boolean;
    canSave: boolean;
    onChange: (draft: ComposeDraft) => void;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const editorId = $props.id();
  const expiryHint = `${editorId}-expiry`;
  const direct = $derived(draft.scope === 'direct');
  const scopeOptions = $derived(Object.entries(SCOPE_LABEL).map(([value, key]) => ({ value, label: t(key) })));
  const levelOptions = $derived(LEVELS.map((value) => ({ value, label: t(LEVEL_LABEL[value]) })));

  function edit<FieldName extends keyof ComposeDraft>(field: FieldName, value: ComposeDraft[FieldName]) {
    onChange({ ...draft, [field]: value });
  }
</script>

<form class="compose-form" method="POST" action="?/send" use:enhance={onSubmit}>
  <Scroller fill padding="var(--bb-space-4)">
    <fieldset class="compose-fields" disabled={status === 'saving'}>
      <Stack gap={4}>
        <Stack gap={2}>
          <Label as="span">{t('admin.notifications.audienceLabel')}</Label>
          <SegmentedControl
            even
            options={scopeOptions}
            label={t('admin.notifications.audienceLabel')}
            value={draft.scope}
            onValueChange={(value) => edit('scope', value as NotificationScope)}
          />
          <input type="hidden" name="scope" value={draft.scope} />
        </Stack>

        <div class="recipients">
          <div class="recipient-options" class:active={direct} inert={!direct} aria-hidden={!direct}>
            <Field fit label={t('admin.notifications.fieldUserId')}>
              <Input
                fill
                mono
                name="target_user_id"
                inputmode="numeric"
                autocomplete="off"
                placeholder={t('admin.notifications.fieldUserIdPlaceholder')}
                disabled={!direct}
                bind:value={() => draft.targetUserId, (value) => edit('targetUserId', String(value ?? ''))}
              />
            </Field>
            <Field fit label={t('admin.notifications.fieldUsername')}>
              <Input
                fill
                name="target_username"
                autocomplete="off"
                placeholder={t('admin.notifications.fieldUsernamePlaceholder')}
                disabled={!direct}
                bind:value={() => draft.targetUsername, (value) => edit('targetUsername', String(value ?? ''))}
              />
            </Field>
          </div>
          <div class="broadcast-hint" class:active={!direct} aria-hidden={direct}>
            <Text size="sm" tone="muted"><bb-fit mode="block">{t('admin.notifications.broadcastHint')}</bb-fit></Text>
          </div>
        </div>

        <Field fit label={t('admin.notifications.fieldTitle')}>
          <Input
            fill
            name="title"
            maxlength={MAX_TITLE_LENGTH}
            required
            placeholder={t('admin.notifications.fieldTitlePlaceholder')}
            bind:value={() => draft.title, (value) => edit('title', String(value ?? ''))}
          />
        </Field>

        <Field fit label={t('admin.notifications.fieldBody')}>
          <Textarea
            fill
            name="body"
            maxlength={MAX_BODY_LENGTH}
            rows={4}
            required
            placeholder={t('admin.notifications.fieldBodyPlaceholder')}
            bind:value={() => draft.body, (value) => edit('body', value)}
          />
        </Field>

        <Field fit label={t('admin.notifications.fieldLevel')}>
          <Select
            fill
            name="level"
            options={levelOptions}
            disabled={status === 'saving'}
            bind:value={() => draft.level, (value) => edit('level', value as NotificationLevel)}
          />
        </Field>

        <Field
          fit
          label={t('admin.notifications.fieldExpires')}
          tag={t('common.optional')}
        >
          <Input
            fill
            mono
            type="datetime-local"
            name="expires_at"
            aria-describedby={expiryHint}
            bind:value={() => draft.expiresAt, (value) => edit('expiresAt', String(value ?? ''))}
          />
          <Text as="small" size="xs" tone="muted" class="expiry-hint" id={expiryHint}>
            <bb-fit mode="block">{t('admin.notifications.expiresHint')}</bb-fit>
          </Text>
        </Field>
      </Stack>
    </fieldset>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={t('admin.notifications.sendCta')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('admin.notifications.sending')}
    savedLabel={t('admin.notifications.sent')}
    errorLabel={t('admin.notifications.sendFailed')}
    {onCancel}
  />
</form>

<style>
  .compose-form {
    display: flex;
    flex-direction: column;
    min-block-size: 0;
    max-block-size: 100%;
  }

  .compose-fields {
    min-inline-size: 0;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .recipients {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
  }

  .recipient-options,
  .broadcast-hint {
    grid-area: 1 / 1;
    opacity: 0;
    visibility: hidden;
    transition: opacity var(--bb-dur-fast) ease,
      visibility 0s linear var(--bb-dur-fast);
  }

  .recipient-options {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-3);
  }

  .recipient-options.active,
  .broadcast-hint.active {
    opacity: 1;
    visibility: visible;
    transition-delay: 0s;
  }

  :global(.compose-fields .expiry-hint) {
    block-size: 2rem;
  }

  @media (prefers-reduced-motion: reduce) {
    .recipient-options,
    .broadcast-hint {
      transition: none;
    }
  }
</style>
