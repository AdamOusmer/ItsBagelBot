<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import {
    getI18n,
    KEY_LABEL_MAX,
    KEY_VALUE_MAX,
    slugifyName
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
        <span class="label"><Text as="span" size="xs" mono>{k.label}</Text></span>
        <Text as="span" size="xs" mono tone="muted" title={t('fetches.keyLast4Title')}>••••{k.last4}</Text>
        <span class="acts">
          <IconButton
            size="sm"
            title={t('fetches.keyRotate')}
            label={t('fetches.keyRotateAria', { label: k.label })}
            onclick={() => {
              rotating = rotating === k.label ? '' : k.label;
              rotateValue = '';
              err = '';
            }}
          ><Icon name="edit" size={15} /></IconButton>
          <IconButton
            size="sm"
            title={t('common.delete')}
            label={t('fetches.keyDeleteAria', { label: k.label })}
            onclick={() => (deleteTarget = k)}
          ><Icon name="trash" size={15} /></IconButton>
        </span>
        {#if rotating === k.label}
          <form class="rotate" onsubmit={submitRotate}>
            <Input
              fill
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
  <div class="empty"><Text size="sm" tone="muted">{t('fetches.keyNoneYet')}</Text></div>
{/if}

<form class="add-key" onsubmit={submitNew}>
  <span class="add-key-field">
    <Input
      fill
      placeholder={t('fetches.keyLabelPh')}
      aria-label={t('fetches.keyLabelAria')}
      autocomplete="off"
      spellcheck="false"
      maxlength={KEY_LABEL_MAX}
      bind:value={newLabel}
    />
  </span>
  <span class="add-key-field">
    <Input
      fill
      type="password"
      placeholder={t('fetches.keyValuePh')}
      aria-label={t('fetches.keyValueNewAria')}
      autocomplete="off"
      spellcheck="false"
      maxlength={KEY_VALUE_MAX}
      bind:value={newValue}
      required
    />
  </span>
  <Button type="submit" variant="secondary" disabled={busy}>{t('fetches.keyAdd')}</Button>
</form>
<Text size="xs" tone="muted">{t('fetches.keyNote')}</Text>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('fetches.keyDeleteTitle', { label: deleteTarget?.label ?? '' })}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  busy={busy}
  onConfirm={confirmDelete}
  onCancel={() => (deleteTarget = null)}
  tone="danger"
>
  {#if referencing.length > 0}
    <Text size="sm">{t('fetches.keyDeleteRefs')}</Text>
    <ul class="ref-list">
      {#each referencing as name (name)}
        <li><Code tone="danger">!{name}</Code></li>
      {/each}
    </ul>
  {:else}
    <Text size="sm" tone="muted">{t('fetches.keyDeleteSafe')}</Text>
  {/if}
</ConfirmDialog>

<style>
  .key-list { list-style: none; margin: 0 0 14px; padding: 0; display: flex; flex-direction: column; }
  .key-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 2px;
    border-bottom: 1px solid var(--bb-border);
    flex-wrap: wrap;
  }
  .key-row:last-child { border-bottom: none; }
  .label { min-width: 120px; }
  .acts { margin-left: auto; display: inline-flex; gap: 8px; }

  .rotate { display: flex; gap: 8px; width: 100%; }

  .empty { margin-bottom: 14px; }

  .add-key { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 8px; }
  .add-key-field { display: flex; flex: 1; min-width: 140px; }

  .ref-list { list-style: none; margin: 8px 0; padding: 0; display: flex; flex-wrap: wrap; gap: 6px; }
</style>
