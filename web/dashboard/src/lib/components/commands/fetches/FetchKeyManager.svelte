<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  import {
    AlertBanner,
    Button,
    ConfirmDialog,
    Icon,
    getI18n,
    KEY_LABEL_MAX,
    KEY_VALUE_MAX,
    slugifyName,
    Code
  } from '@bagel/kit';
  import type { FetchKeyView } from '$lib/server/fetches-store';

  const { t } = getI18n();

  let {
    keys,
    references,
    busy = false,
    onSetKey,
    onDeleteKey
  }: {
    keys: FetchKeyView[];
    references: Record<string, string[]>;
    busy?: boolean;
    onSetKey: (label: string, value: string) => void;
    onDeleteKey: (label: string) => void;
  } = $props();

  let newLabel = $state('');
  let newValue = $state('');
  let err = $state('');

  let rotating = $state('');
  let rotateValue = $state('');

  let deleteTarget = $state<FetchKeyView | null>(null);

  const referencing = $derived(deleteTarget ? (references[deleteTarget.label] ?? []) : []);

  function submitNew(e: SubmitEvent) {
    e.preventDefault();
    const label = slugifyName(newLabel);
    if (!label) {
      err = t('fetches.keyErrLabel');
      return;
    }
    if (!newValue.trim()) {
      err = t('fetches.keyErrValue');
      return;
    }
    if (keys.some((k) => k.label === label)) {
      err = t('fetches.keyErrExists', { label });
      return;
    }
    err = '';
    onSetKey(label, newValue);
    newLabel = '';
    newValue = '';
  }

  function submitRotate(e: SubmitEvent) {
    e.preventDefault();
    if (!rotateValue.trim()) {
      err = t('fetches.keyErrValue');
      return;
    }
    err = '';
    onSetKey(rotating, rotateValue);
    rotating = '';
    rotateValue = '';
  }

  function confirmDelete() {
    if (!deleteTarget) return;
    onDeleteKey(deleteTarget.label);
    deleteTarget = null;
  }
</script>

{#if err}
  <AlertBanner>{err}</AlertBanner>
{/if}

{#if keys.length > 0}
  <ul class="key-list">
    {#each keys.toSorted((a, b) => a.label.localeCompare(b.label)) as k (k.label)}
      <li class="key-row">
        <span class="label">{k.label}</span>
        <span class="last4" title={t('fetches.keyLast4Title')}>••••{k.last4}</span>
        <span class="acts">
          <Button
            type="button"
            variant="icon" size="sm"
            title={t('fetches.keyRotate')}
            aria-label={t('fetches.keyRotateAria', { label: k.label })}
            onclick={() => {
              rotating = rotating === k.label ? '' : k.label;
              rotateValue = '';
              err = '';
            }}
           ><Icon name="edit" size={15} /></Button>
          <Button
            type="button"
            variant="icon" size="sm"
            title={t('common.delete')}
            aria-label={t('fetches.keyDeleteAria', { label: k.label })}
            onclick={() => (deleteTarget = k)}
           ><Icon name="trash" size={15} /></Button>
        </span>
        {#if rotating === k.label}
          <form class="rotate" onsubmit={submitRotate}>
            <input
              class="bb-input bb-input--fill"
              type="password"
              placeholder={t('fetches.keyValuePh')}
              aria-label={t('fetches.keyValueAria', { label: k.label })}
              autocomplete="off"
              spellcheck="false"
              maxlength={KEY_VALUE_MAX}
              bind:value={rotateValue}
              required
            />
            <Button type="submit" variant="primary" disabled={busy}>{t('fetches.keySave')}</Button>
          </form>
        {/if}
      </li>
    {/each}
  </ul>
{:else}
  <p class="empty">{t('fetches.keyNoneYet')}</p>
{/if}

<form class="add-key" onsubmit={submitNew}>
  <input
    class="bb-input bb-input--fill add-key-field"
    placeholder={t('fetches.keyLabelPh')}
    aria-label={t('fetches.keyLabelAria')}
    autocomplete="off"
    spellcheck="false"
    maxlength={KEY_LABEL_MAX}
    bind:value={newLabel}
  />
  <input
    class="bb-input bb-input--fill add-key-field"
    type="password"
    placeholder={t('fetches.keyValuePh')}
    aria-label={t('fetches.keyValueNewAria')}
    autocomplete="off"
    spellcheck="false"
    maxlength={KEY_VALUE_MAX}
    bind:value={newValue}
    required
  />
  <Button type="submit" variant="secondary" disabled={busy}>{t('fetches.keyAdd')}</Button>
</form>
<small class="note">{t('fetches.keyNote')}</small>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('fetches.keyDeleteTitle', { label: deleteTarget?.label ?? '' })}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  danger
  busy={busy}
  onConfirm={confirmDelete}
  onCancel={() => (deleteTarget = null)}
>
  {#if referencing.length > 0}
    <p class="ref-warn">{t('fetches.keyDeleteRefs')}</p>
    <ul class="ref-list">
      {#each referencing as name (name)}
        <li><Code tone="danger">!{name}</Code></li>
      {/each}
    </ul>
  {:else}
    <p class="ref-note">{t('fetches.keyDeleteSafe')}</p>
  {/if}
</ConfirmDialog>

<style>
  .key-list { list-style: none; margin: 0 0 14px; padding: 0; display: flex; flex-direction: column; }
  .key-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 2px;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    flex-wrap: wrap;
  }
  .key-row:last-child { border-bottom: none; }
  .label { font-family: var(--bb-font-mono); font-size: 12.5px; color: var(--bb-white); min-width: 120px; }
  .last4 { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); letter-spacing: 0.08em; }
  .acts { margin-left: auto; display: inline-flex; gap: 8px; }

  .rotate { display: flex; gap: 8px; width: 100%; }

  .empty { margin: 0 0 14px; font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); font-style: italic; }

  .add-key { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  .add-key-field { min-width: 140px; }

  .note { display: block; margin-top: 8px; font-family: var(--bb-font-body); font-size: 11px; line-height: 1.5; color: var(--bb-muted); opacity: 0.7; }

  .ref-warn { margin: 0 0 8px; font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .ref-list { list-style: none; margin: 0 0 8px; padding: 0; display: flex; flex-wrap: wrap; gap: 6px; }
  .ref-note { margin: 0; font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }
</style>
