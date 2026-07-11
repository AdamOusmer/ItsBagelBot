<script lang="ts">
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, getI18n } from '@bagel/shared';

  let {
    draft = $bindable<{ text: string; quoteDate: string }>(),
    busy = false,
    onCancel,
    onSubmit
  }: {
    draft: { text: string; quoteDate: string };
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const MAX = 450;
</script>

<form method="POST" action="?/add" class="editor" novalidate use:enhance={onSubmit}>
  <label class="field">
    <span>{t('quotes.fieldQuote')}</span>
    <textarea
      class="search quote-area"
      name="text"
      placeholder={t('quotes.addPlaceholder')}
      maxlength={MAX}
      required
      rows="4"
      bind:value={draft.text}
    ></textarea>
    <small>{draft.text.length}/{MAX}</small>
  </label>

  <label class="field">
    <span>{t('quotes.fieldDay')}</span>
    <input class="search date-input" type="date" name="quote_date" required bind:value={draft.quoteDate} />
    <small>{t('quotes.fieldDayHint')}</small>
  </label>

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="submit" class="btn primary" disabled={busy || !draft.text.trim() || !draft.quoteDate}>
      <Icon name="check" size={14} />
      {busy ? t('quotes.saving') : t('quotes.addBtn')}
    </button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }
  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 16px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }
  .field:first-child small { text-align: right; }

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
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
