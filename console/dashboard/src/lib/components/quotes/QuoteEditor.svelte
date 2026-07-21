<script lang="ts">
  // Inline quote editor, rendered inside the page inspector. In "add" mode it
  // saves a new quote; in "edit" mode it rewrites an existing quote's body
  // and day in place (the number survives). Each control is wrapped in the
  // shared <Field> (a real <label>, so the input is labelled), and the
  // Save/Cancel actions live in the form so the editor stays self-contained
  // within the page's docked inspector.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Field, Button, getI18n } from '@bagel/shared';

  let {
    draft = $bindable<{ text: string; quoteDate: string }>(),
    number = null,
    busy = false,
    onCancel,
    onSubmit
  }: {
    draft: { text: string; quoteDate: string };
    // The quote being rewritten; null means the editor adds a new one.
    number?: number | null;
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const MAX = 450;

  const editing = $derived(number !== null);
  const valid = $derived(draft.text.trim().length > 0 && /^\d{4}-\d{2}-\d{2}$/.test(draft.quoteDate));
</script>

<form method="POST" action={editing ? '?/edit' : '?/add'} class="editor" novalidate use:enhance={onSubmit}>
  {#if editing}
    <input type="hidden" name="number" value={number} />
  {/if}

  <Field label={t('quotes.fieldQuote')}>
    <textarea
      class="search quote-area"
      name="text"
      placeholder={t('quotes.addPlaceholder')}
      maxlength={MAX}
      required
      rows="4"
      bind:value={draft.text}
    ></textarea>
    <small class="counter">{draft.text.length}/{MAX}</small>
  </Field>

  <Field label={t('quotes.fieldDay')}>
    <input class="search date-input" type="date" name="quote_date" required bind:value={draft.quoteDate} />
    <small class="hint">{t('quotes.fieldDayHint')}</small>
  </Field>

  <div class="actions">
    <Button variant="ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</Button>
    <Button variant="primary" type="submit" icon="check" loading={busy} disabled={!valid}>
      {editing ? t('quotes.editBtn') : t('quotes.addBtn')}
    </Button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }
  .counter { display: block; text-align: right; color: var(--bb-muted); opacity: 0.7; font-size: 11px; margin-top: 4px; }
  .hint { display: block; color: var(--bb-muted); opacity: 0.7; font-size: 11px; margin-top: 4px; }

  .quote-area,
  .date-input {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    padding: 9px 11px;
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.35);
    color: var(--bb-white);
  }
  .quote-area { resize: vertical; min-height: 92px; line-height: 1.5; }
  .date-input { color-scheme: dark; }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 6px; }
  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions :global(.btn) { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
