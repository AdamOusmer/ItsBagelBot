<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Field, getI18n } from '@bagel/kit';

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
    <textarea
      class="bb-input quote-area"
      name="text"
      placeholder={t('quotes.addPlaceholder')}
      maxlength={MAX}
      required
      data-invalid={textError ? '' : undefined}
      aria-invalid={textError ? 'true' : undefined}
      aria-describedby={textError ? 'quote-text-err' : undefined}
      rows="4"
      bind:value={draft.text}
      onblur={() => (touched.text = true)}
    ></textarea>
    <small class="counter">{draft.text.length}/{MAX}</small>
  </Field>

  <Field label={t('quotes.fieldDay')} error={dayError} errorId="quote-day-err">
    <input
      class="bb-input date-input"
      type="date"
      name="quote_date"
      required
      data-invalid={dayError ? '' : undefined}
      aria-invalid={dayError ? 'true' : undefined}
      aria-describedby={dayError ? 'quote-day-hint quote-day-err' : 'quote-day-hint'}
      bind:value={draft.quoteDate}
      onblur={() => (touched.day = true)}
    />
    <small id="quote-day-hint" class="hint">{t('quotes.fieldDayHint')}</small>
  </Field>

  {#if addedBy}
    <p class="added-by">{t('quotes.addedBy')} @{addedBy}</p>
  {/if}
</div>

<style>
  .editor { padding: 4px 2px 2px; }
  .counter { display: block; text-align: right; color: var(--bb-muted); opacity: 0.7; font-size: 11px; margin-top: 4px; }
  .hint { display: block; color: var(--bb-muted); opacity: 0.7; font-size: 11px; margin-top: 4px; }
  .added-by { margin: 8px 0 0; font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }

  .quote-area,
  .date-input {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    padding: 9px 11px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: rgba(0, 0, 0, 0.35);
    color: var(--bb-white);
  }
  .quote-area { resize: vertical; min-height: 92px; line-height: 1.5; }
  .date-input { color-scheme: dark; }
</style>
