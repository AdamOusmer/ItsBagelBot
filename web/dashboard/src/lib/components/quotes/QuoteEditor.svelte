<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, Field, Input, Text, Textarea, getI18n } from '@bagel/kit';
  import { focusFirstInvalid } from '@bagel/kit';

  let {
    draft = $bindable<{ text: string; quoteDate: string }>(),
    number = null,
    busy = false,
    onCancel,
    onSubmit
  }: {
    draft: { text: string; quoteDate: string };
    number?: number | null;
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const MAX = 450;

  const editing = $derived(number !== null);
  let attempted = $state(false);
  let formEl = $state<HTMLFormElement | null>(null);
  const textError = $derived(attempted && !draft.text.trim() ? t('quotes.errText') : undefined);
  const dayError = $derived(
    attempted && !/^\d{4}-\d{2}-\d{2}$/.test(draft.quoteDate) ? t('quotes.errDay') : undefined
  );

  const submit: SubmitFunction = (input) => {
    attempted = true;
    if (!draft.text.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(draft.quoteDate)) {
      input.cancel();
      void focusFirstInvalid(formEl);
      return;
    }
    return onSubmit(input);
  };
</script>

<form
  method="POST"
  action={editing ? '?/edit' : '?/add'}
  class="editor"
  novalidate
  use:enhance={submit}
  bind:this={formEl}
>
  {#if editing}
    <input type="hidden" name="number" value={number} />
  {/if}

  <Field label={t('quotes.fieldQuote')} error={textError} errorId="quote-text-err">
    <Textarea
      name="text"
      placeholder={t('quotes.addPlaceholder')}
      maxlength={MAX}
      required
      invalid={!!textError}
      aria-invalid={textError ? 'true' : undefined}
      aria-describedby={textError ? 'quote-text-err' : undefined}
      rows={4}
      bind:value={draft.text}
    />
    <span class="counter"><Text as="small" size="xs" tone="muted">{draft.text.length}/{MAX}</Text></span>
  </Field>

  <Field
    label={t('quotes.fieldDay')}
    hint={t('quotes.fieldDayHint')}
    hintId="quote-day-hint"
    error={dayError}
    errorId="quote-day-err"
  >
    <Input
      type="date"
      name="quote_date"
      required
      invalid={!!dayError}
      aria-invalid={dayError ? 'true' : undefined}
      aria-describedby={dayError ? 'quote-day-hint quote-day-err' : 'quote-day-hint'}
      bind:value={draft.quoteDate}
    />
  </Field>

  <div class="actions">
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button variant="primary" type="submit" loading={busy}>
      {editing ? t('quotes.editBtn') : t('quotes.addBtn')}
    </Button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }
  .counter { display: block; text-align: right; margin-top: 4px; }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 6px; }
  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions { --btn-w: 100%; --btn-justify: center; --btn-min-h: 44px; }
  }
</style>
