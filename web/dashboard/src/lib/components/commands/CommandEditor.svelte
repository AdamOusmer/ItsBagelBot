<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Field,
    Grid,
    FieldError,
    Scroller,
    EditorFooter,
    Switch,
    PERMS,
    tPerm,
    validateCommand,
    commandContentSnapshot,
    normName,
    COOLDOWN_MAX,
    RESPONSE_MAX_LINES,
    getI18n,
    translateValidationMessage,
    type CommandErrors
  } from '@bagel/kit';
  import { Checkbox } from '@bagel/kit';
  import AliasChips from './AliasChips.svelte';
  import ResponseEditor from './ResponseEditor.svelte';
  import type { SourceDef } from './fetches/FetchSourcePicker.svelte';
  import ChatPreview from './ChatPreview.svelte';
  import { draftKey, type CommandDraft } from './drafts';
  import { focusFirstInvalid } from '@bagel/kit';

  let {
    draft = $bindable<CommandDraft>(),
    serverErrors = null as CommandErrors | null,
    status = 'idle' as 'idle' | 'saving' | 'saved' | 'error' | 'conflict',
    dirty = false,
    canSave = true,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged,
    onCancel,
    onSubmit,
    liveActive = false,
    onToggleActive
  }: {
    draft: CommandDraft;
    serverErrors?: CommandErrors | null;
    status?: 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
    dirty?: boolean;
    canSave?: boolean;
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    liveActive?: boolean;
    onToggleActive?: (next: boolean) => void;
  } = $props();

  const busy = $derived(status === 'saving');

  const { t } = getI18n();
  const validationT = (key: string, params?: Record<string, string | number>) => t(key as any, params);

  let aliasDraft = $state('');
  let chips = $state<ReturnType<typeof AliasChips>>();
  let clientErrors = $state<CommandErrors>({});
  let formEl = $state<HTMLFormElement | null>(null);
  const errors = $derived<CommandErrors>(Object.fromEntries(
    Object.entries({ ...(serverErrors ?? {}), ...clientErrors }).map(([field, message]) => [
      field,
      translateValidationMessage(message, validationT)
    ])
  ) as CommandErrors);

  const key = draftKey(draft.originalName, draft.edit);
  const initial = commandContentSnapshot(draft);
  $effect(() => {
    const current = commandContentSnapshot(draft);
    if (current === initial) return;
    try {
      sessionStorage.setItem(key, current);
    } catch {}
  });

  const submit: SubmitFunction = (input) => {
    chips?.commit();
    clientErrors = validateCommand({
      name: normName(draft.name),
      aliases: draft.aliases.map(normName).filter(Boolean),
      response: draft.response,
      cooldown: Math.floor(Number(draft.cooldown) || 0),
      allowedUserId: draft.allowed_user_id.replace(/\D/g, ''),
      bumpCounter: normName(draft.bump_counter)
    });
    if (Object.keys(clientErrors).length) {
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    return onSubmit(input);
  };
</script>

<form method="POST" action="?/save" class="editor-form" novalidate use:enhance={submit} bind:this={formEl}>
  <Scroller fill padding="16px" smooth>
   <div class="editor">
  {#if draft.edit}
    <input type="hidden" name="edit" value="1" />
    <input type="hidden" name="original_name" value={draft.originalName} />
  {/if}

  <Field
    label={t('commandEditor.name')}
    hint={draft.edit ? t('commandEditor.renameHint') : undefined}
    error={errors.name}
    errorId="command-name-err"
  >
    <input
      class="bb-input"
      name="name"
      placeholder={t('commandEditor.namePlaceholder')}
      required
      data-invalid={errors.name ? '' : undefined}
      aria-invalid={errors.name ? 'true' : undefined}
      aria-describedby={errors.name ? 'command-name-err' : undefined}
      bind:value={draft.name}
    />
  </Field>

  <Field label={t('commandEditor.altNames')} tag={t('common.optional')}>
    <AliasChips bind:this={chips} bind:aliases={draft.aliases} bind:draft={aliasDraft} commandName={draft.name} />
    {#each draft.aliases as a}
      <input type="hidden" name="aliases" value={a} />
    {/each}
    <FieldError message={errors.aliases} />
  </Field>

  <Field label={t('commandEditor.response')} error={errors.response} errorId="command-response-err">
    <ResponseEditor
      bind:value={draft.response}
      surface="custom"
      maxLines={RESPONSE_MAX_LINES}
      required
      invalid={!!errors.response}
      describedby={errors.response ? 'command-response-err' : undefined}
      {fetchDefs}
      {fetchKeys}
      onFetchDefsChanged={onFetchDefsChanged}
    />
  </Field>

  <ChatPreview name={draft.name} response={draft.response} />

  <Grid cols={2} gap={3}>
    <Field label={t('commandEditor.access')}>
      <select class="bb-input" name="perm" bind:value={draft.perm}>
        {#each PERMS as p}
          <option value={p}>{tPerm(t, p)}</option>
        {/each}
      </select>
    </Field>

    <Field label={t('commandEditor.cooldownS')} error={errors.cooldown} errorId="command-cooldown-err">
      <input
        class="bb-input"
        type="number"
        name="cooldown"
        min="0"
        max={COOLDOWN_MAX}
        data-invalid={errors.cooldown ? '' : undefined}
        aria-invalid={errors.cooldown ? 'true' : undefined}
        aria-describedby={errors.cooldown ? 'command-cooldown-err' : undefined}
        bind:value={draft.cooldown}
      />
    </Field>
  </Grid>

  <Field
    label={t('commandEditor.restrictUser')}
    tag={t('common.optional')}
    error={errors.allowed_user_id}
    errorId="command-user-err"
  >
    <input
      class="bb-input"
      name="allowed_user_id"
      inputmode="numeric"
      placeholder={t('commandEditor.restrictPlaceholder')}
      data-invalid={errors.allowed_user_id ? '' : undefined}
      aria-invalid={errors.allowed_user_id ? 'true' : undefined}
      aria-describedby={errors.allowed_user_id ? 'command-user-err' : undefined}
      bind:value={draft.allowed_user_id}
    />
  </Field>

  <div class="check">
    {#if draft.edit && onToggleActive}
      <input type="hidden" name="is_active" value={liveActive ? 'on' : ''} />
      <div class="live-active">
        <span class="live-lbl">{t('commandEditor.active')}</span>
        <Switch
          checked={liveActive}
          pending={busy}
          onchange={() => onToggleActive(!liveActive)}
          label={t('commandEditor.active')}
        />
      </div>
    {:else}
      <Checkbox name="is_active" bind:checked={draft.is_active}>{t('commandEditor.active')}</Checkbox>
    {/if}
  </div>

  <div class="check">
    <Checkbox name="stream_online_only" bind:checked={draft.stream_online_only}>{t('commandEditor.onlyWhileLive')}</Checkbox>
  </div>

  <Field
    label={t('commandEditor.bumpCounter')}
    tag={t('common.optional')}
    hint={t('commandEditor.bumpCounterHint')}
    error={errors.bump_counter}
    errorId="command-bump-counter-err"
  >
    <input
      class="bb-input"
      name="bump_counter"
      maxlength="64"
      placeholder={t('commandEditor.bumpCounterPlaceholder')}
      data-invalid={errors.bump_counter ? '' : undefined}
      aria-invalid={errors.bump_counter ? 'true' : undefined}
      aria-describedby={errors.bump_counter ? 'command-bump-counter-err' : undefined}
      bind:value={draft.bump_counter}
    />
  </Field>
   </div>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={draft.edit ? t('commandEditor.saveChanges') : t('commandEditor.create')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('commandEditor.saving')}
    savedLabel={t('commands.saved')}
    errorLabel={t('commands.toastSaveFailed')}
    dirtyLabel={t('commands.unsavedChanges')}
    {onCancel}
  />
</form>

<style>
  .editor-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .editor { padding: 4px 2px 2px; }


  .check { margin: 4px 0 14px; --bb-check-align: center; }
  .live-active {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .live-lbl {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white, var(--bb-text, #e8e0d6));
  }
</style>
