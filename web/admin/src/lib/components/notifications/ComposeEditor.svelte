<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The compose half of the notifications inspector. The <form> wraps the fields
  // AND the EditorFooter (the footer's Save is this form's submit button), and
  // the footer is a sibling after the scroll area so it never scrolls out of
  // view -- the same shape as StaffEditor.
  //
  // The fields post by NAME rather than through hidden mirrors: the action reads
  // `scope`, `target_user_id`, `target_username`, `title`, `body`, `level` and
  // `expires_at` straight off the FormData, so the composer keeps working with
  // JavaScript off.
  import type { SubmitFunction } from '@sveltejs/kit';
  import { enhance } from '$app/forms';
  import RadioGroup from '@bagel/shared/components/RadioGroup.svelte';
  import Field from '@bagel/shared/components/Field.svelte';
  import Scroller from '@bagel/shared/components/Scroller.svelte';
  import EditorFooter from '@bagel/shared/components/EditorFooter.svelte';
  import type { InspectorStatus } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
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
  <Scroller fill padding="18px" data-lenis-prevent>
    <div class="body">
      <section class="block">
        <h3 class="block-label">{t('admin.notifications.audienceLabel')}</h3>
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
        <!-- Either identifier will do; the send path resolves the username when
             only that is given, so neither field is individually required. -->
        <Field label={t('admin.notifications.fieldUserId')}>
          <input
            class="text-input"
            type="text"
            name="target_user_id"
            inputmode="numeric"
            autocomplete="off"
            placeholder={t('admin.notifications.fieldUserIdPlaceholder')}
            bind:value={draft.targetUserId}
          />
        </Field>
        <Field label={t('admin.notifications.fieldUsername')}>
          <input
            class="text-input"
            type="text"
            name="target_username"
            autocomplete="off"
            placeholder={t('admin.notifications.fieldUsernamePlaceholder')}
            bind:value={draft.targetUsername}
          />
        </Field>
      {/if}

      <Field label={t('admin.notifications.fieldTitle')}>
        <input
          class="text-input"
          type="text"
          name="title"
          maxlength="120"
          required
          placeholder={t('admin.notifications.fieldTitlePlaceholder')}
          bind:value={draft.title}
        />
      </Field>

      <Field label={t('admin.notifications.fieldBody')}>
        <textarea
          class="text-input"
          name="body"
          maxlength="2000"
          rows="4"
          required
          placeholder={t('admin.notifications.fieldBodyPlaceholder')}
          bind:value={draft.body}
        ></textarea>
      </Field>

      <Field label={t('admin.notifications.fieldLevel')}>
        <select class="text-input" name="level" bind:value={draft.level}>
          {#each LEVELS as level (level)}
            <option value={level}>{t(LEVEL_LABEL[level as NotificationLevel])}</option>
          {/each}
        </select>
      </Field>

      <Field label={t('admin.notifications.fieldExpires')} tag={t('common.optional')}>
        <input
          class="text-input"
          type="datetime-local"
          name="expires_at"
          bind:value={draft.expiresAt}
        />
      </Field>
      <p class="note">{t('admin.notifications.expiresHint')}</p>
    </div>
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
  .body {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
    margin-bottom: 14px;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 0;
  }
</style>
