<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Field, Input, Text, Textarea } from '@bagel/ui/svelte';
  import { getI18n } from '@bagel/kit';

  let {
    draft = $bindable<{ text: string; quoteDate: string }>(),
    attempted = false,
    addedBy = ''
  }: {
    draft: { text: string; quoteDate: string };
    attempted?: boolean;
    addedBy?: string;
  } = $props();

  const { t } = getI18n();
  const MAX = 450;

  let touched = $state({ text: false, day: false });
  const textError = $derived(
    (attempted || touched.text) && !draft.text.trim() ? t('quotes.errText') : undefined
  );
  const dayError = $derived(
    (attempted || touched.day) && !/^\d{4}-\d{2}-\d{2}$/.test(draft.quoteDate)
      ? t('quotes.errDay')
      : undefined
  );
</script>

<div class="editor">
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
      onblur={() => (touched.text = true)}
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
      onblur={() => (touched.day = true)}
    />
  </Field>

  {#if addedBy}
    <div class="added-by"><Text size="xs" mono tone="muted">{t('quotes.addedBy')} @{addedBy}</Text></div>
  {/if}
</div>

<style>
  .editor { padding: 4px 2px 2px; }
  .counter { display: block; text-align: right; margin-top: 4px; }
  .added-by { margin-top: 8px; }
</style>
