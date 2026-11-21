<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { counterComplete, type CounterDraft } from './counter-draft';

  let { draft, creating, scope, status, dirty, busy, onEdit, onCancel, onDelete, submit }: {
    draft: CounterDraft;
    creating: boolean;
    scope?: string;
    status: 'idle' | 'saving' | 'saved' | 'error';
    dirty: boolean;
    busy: boolean;
    onEdit: (draft: CounterDraft) => void;
    onCancel: () => void;
    onDelete?: () => void;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const field = $derived(creating ? 'name' : 'value');
  const canSave = $derived(!busy && dirty && counterComplete(draft));
  const hintId = $props.id();
</script>

<form class="editor" method="POST" action={creating ? '?/create' : '?/set'} use:enhance={submit}>
  {#if !creating}<input type="hidden" name="name" value={draft.name} />{/if}

  <Scroller fill padding="var(--bb-space-4)">
    <Stack gap={4}>
      {#if scope}
        <Text size="xs" tone="muted" mono>{t('admin.counters.rowMeta', { scope })}</Text>
      {/if}
      <Field fit label={t(creating ? 'admin.counters.fieldName' : 'admin.counters.fieldValue')}>
        <Input
          id="counter-field"
          fill mono
          name={field}
          required
          readonly={busy}
          maxlength={creating ? 64 : undefined}
          inputmode={creating ? undefined : 'numeric'}
          placeholder={creating ? t('admin.counters.fieldNamePlaceholder') : undefined}
          aria-describedby={creating ? undefined : hintId}
          bind:value={() => draft[field], (value) => onEdit({ ...draft, [field]: String(value ?? '') })}
        />
      </Field>
      {#if !creating}
        <Text as="div" id={hintId} size="sm" tone="muted" style="block-size: 2.5rem">
          <bb-fit mode="block"><span data-fit>{t('admin.counters.valueHint')}</span></bb-fit>
        </Text>
        <Button disabled={busy || !onDelete} onclick={onDelete} variant="secondary" tone="danger">{t('common.delete')}</Button>
      {/if}
    </Stack>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    {canSave}
    saveLabel={creating ? t('admin.counters.addCta') : t('common.save')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('admin.saving')}
    savedLabel={t('admin.saved')}
    dirtyLabel={t('admin.unsaved')}
    errorLabel={t('admin.saveFailed')}
    {onCancel}
  />
</form>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-block-size: 0;
    max-block-size: 100%;
  }
</style>
