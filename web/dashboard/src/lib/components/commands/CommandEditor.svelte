<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Field,
    Grid,
    FieldError,
    Input,
    Select,
    Text,
    Scroller,
    EditorFooter,
    Switch
  } from '@bagel/ui/svelte';
  import {
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
  import { Checkbox } from '@bagel/ui/svelte';
  import AliasChips from './AliasChips.svelte';
  import ResponseEditor from './ResponseEditor.svelte';
  import type { SourceDef } from './fetches/FetchSourcePicker.svelte';
  import ChatPreview from './ChatPreview.svelte';
  import { nameConflict } from './name-conflict';
  import { draftRef, loadDraft, saveDraft, type BoardId, type CommandDraft } from './drafts';
  import { focusFirstInvalid } from '@bagel/ui/svelte';

  let {
    draft = $bindable<CommandDraft>(),
    board,
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
    onToggleActive,
    customNames = [],
    onOpenExisting
  }: {
    draft: CommandDraft;
    board: BoardId;
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
    customNames?: readonly string[];
    onOpenExisting?: (name: string) => void;
  } = $props();

  const busy = $derived(status === 'saving');

  const { t } = getI18n();
  const validationT = (key: string, params?: Record<string, string | number>) => t(key as any, params);

  let aliasDraft = $state('');
  let chips = $state<ReturnType<typeof AliasChips>>();
  let clientErrors = $state<CommandErrors>({});
  let formEl = $state<HTMLFormElement | null>(null);
  const conflict = $derived(nameConflict(draft, customNames));
  const conflictMessage = $derived(
    conflict === null
      ? undefined
      : conflict.kind === 'builtin'
        ? t('commands.errBuiltinName')
        : t('commands.errNameTaken', { name: conflict.name })
  );
  const errors = $derived<CommandErrors>({
    ...(Object.fromEntries(
      Object.entries({ ...(serverErrors ?? {}), ...clientErrors }).map(([field, message]) => [
        field,
        translateValidationMessage(message, validationT)
      ])
    ) as CommandErrors),
    ...(conflictMessage ? { name: conflictMessage } : {})
  });

  let nameEl = $state<HTMLInputElement>();
  function setNameEl(node: HTMLInputElement) {
    nameEl = node;
  }
  onMount(() => {
    let frames = 0;
    let raf = 0;
    const settle = () => {
      if (++frames < 3) raf = requestAnimationFrame(settle);
      else nameEl?.focus();
    };
    raf = requestAnimationFrame(settle);
    return () => cancelAnimationFrame(raf);
  });

  // Persist every editor field. Active is draft state for creation, but a live
  // toggle on an existing command must not count as an unsaved content edit.
  // svelte-ignore state_referenced_locally
  const ref = draftRef(board, draft);
  const initial = draft.edit ? commandContentSnapshot(draft) : JSON.stringify(draft);
  const storedAtOpen = loadDraft(ref);
  $effect(() => {
    const current = draft.edit ? commandContentSnapshot(draft) : JSON.stringify(draft);
    saveDraft(ref, current === initial ? storedAtOpen : draft);
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
    if (conflictMessage || Object.keys(clientErrors).length) {
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

  <div class="name-wrap">
  <Field
    label={t('commandEditor.name')}
    hint={draft.edit ? t('commandEditor.renameHint') : undefined}
    error={errors.name}
    errorId="command-name-err"
  >
    <Input
      {@attach setNameEl}
      name="name"
      placeholder={t('commandEditor.namePlaceholder')}
      required
      invalid={!!errors.name}
      aria-invalid={errors.name ? 'true' : undefined}
      aria-describedby={errors.name ? 'command-name-err' : undefined}
      bind:value={draft.name}
    />
  </Field>
  <button
    type="button"
    class="open-existing"
    class:on={conflict?.kind === 'taken' && !!onOpenExisting}
    onclick={() => conflict?.kind === 'taken' && onOpenExisting?.(conflict.name)}
  >{t('commands.openExisting')}</button>
  </div>

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
      <Select
        fill
        name="perm" bind:value={draft.perm}
        options={PERMS.map((p) => ({ value: p, label: tPerm(t, p) }))}
      />
    </Field>

    <Field label={t('commandEditor.cooldownS')} error={errors.cooldown} errorId="command-cooldown-err">
      <Input
        type="number"
        name="cooldown"
        min={0}
        max={COOLDOWN_MAX}
        invalid={!!errors.cooldown}
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
    <Input
      name="allowed_user_id"
      inputmode="numeric"
      placeholder={t('commandEditor.restrictPlaceholder')}
      invalid={!!errors.allowed_user_id}
      aria-invalid={errors.allowed_user_id ? 'true' : undefined}
      aria-describedby={errors.allowed_user_id ? 'command-user-err' : undefined}
      bind:value={draft.allowed_user_id}
    />
  </Field>

  <div class="check">
    {#if draft.edit && onToggleActive}
      <input type="hidden" name="is_active" value={liveActive ? 'on' : ''} />
      <div class="live-active">
        <Text as="span" size="sm">{t('commandEditor.active')}</Text>
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
    <Input
      name="bump_counter"
      maxlength={64}
      placeholder={t('commandEditor.bumpCounterPlaceholder')}
      invalid={!!errors.bump_counter}
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

  .name-wrap { position: relative; }
  .open-existing {
    position: absolute;
    top: 0;
    right: 0;
    padding: 0;
    border: none;
    background: none;
    font-family: var(--bb-font-body);
    font-size: var(--bb-text-xs);
    color: var(--bb-green-glow);
    text-decoration: underline;
    cursor: pointer;
    visibility: hidden;
    opacity: 0;
  }
  .open-existing.on { visibility: visible; opacity: 1; }

  .check { margin: 4px 0 14px; --bb-check-align: center; }
  .live-active {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
</style>
