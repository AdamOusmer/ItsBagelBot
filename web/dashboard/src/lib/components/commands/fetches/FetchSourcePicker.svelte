<script module lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  export interface SourceDef {
    name: string;
    url: string;
    json_path: string[];
    key_label: string;
  }
</script>

<script lang="ts">
  import { deserialize } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Code from '@bagel/ui/svelte/Code.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import FieldError from '@bagel/ui/svelte/FieldError.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { focusFirstInvalid } from '@bagel/ui/svelte/forms';
  import {
    getI18n,
    slugifyName,
    buildJsonPath,
    DEFS_PER_BROADCASTER
  } from '@bagel/kit';
  import JsonTree from './JsonTree.svelte';

  const { t } = getI18n();

  let {
    defs = [],
    keys = [],
    onInsert,
    onDefsChanged
  }: {
    defs?: SourceDef[];
    keys?: { label: string }[];
    onInsert: (token: string) => void;
    onDefsChanged?: (defs: SourceDef[]) => void;
  } = $props();

  let open = $state(false);
  let building = $state(false);
  let btnEl = $state<HTMLElement>();
  let panelErr = $state('');

  function toggle() {
    open = !open;
    if (!open) return;
    armedDelete = '';
    panelErr = '';
  }

  function setAnchor(node: HTMLElement) {
    btnEl = node;
  }

  function tokenFor(name: string): string {
    return `{urlfetch:${name}}`;
  }

  function pick(name: string) {
    onInsert(tokenFor(name));
    open = false;
  }

  let displayName = $state('');
  let slug = $state('');
  let slugTouched = $state(false);
  let url = $state('');
  let keyLabel = $state('');
  let sample = $state('');
  let path = $state<string[]>([]);
  let pathPicked = $state(false);
  let fetching = $state(false);
  let creating = $state(false);
  let notice = $state('');
  let err = $state('');
  let showPaste = $state(false);

  const atQuota = $derived(defs.length >= DEFS_PER_BROADCASTER);
  let nameAttempted = $state(false);
  let urlAttempted = $state(false);
  let buildEl = $state<HTMLDivElement | null>(null);
  const nameError = $derived(nameAttempted && !slug ? t('fetches.errNameRequired') : undefined);
  const urlError = $derived(urlAttempted && !url.trim() ? t('fetches.errUrlRequired') : undefined);

  function onDisplayName() {
    if (!slugTouched) slug = slugifyName(displayName);
  }

  function openBuilder() {
    displayName = '';
    slug = '';
    slugTouched = false;
    url = '';
    keyLabel = '';
    sample = '';
    path = [];
    pathPicked = false;
    notice = '';
    err = '';
    showPaste = false;
    nameAttempted = false;
    urlAttempted = false;
    open = false;
    building = true;
  }

  function draftForm(): FormData {
    const f = new FormData();
    f.set('name', slug);
    f.set('url', url.trim());
    f.set('kind', pathPicked && path.length > 0 ? 'json' : 'plain');
    f.set('path', buildJsonPath(path));
    f.set('key_label', keyLabel);
    f.set('edit', '0');
    return f;
  }

  async function post(action: string, body: FormData) {
    const res = await fetch(`/commands?/${action}`, { method: 'POST', body });
    return deserialize(await res.text());
  }

  async function fetchSample() {
    urlAttempted = true;
    if (!url.trim()) {
      void focusFirstInvalid(buildEl);
      return;
    }
    err = '';
    notice = '';
    fetching = true;
    try {
      const f = draftForm();
      f.set('kind', 'plain');
      f.set('path', '');
      const r = await post('testfetch', f);
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; sample?: string; status?: string; error?: string }
        | undefined;
      if (d?.sample) {
        sample = d.sample;
        showPaste = false;
        if (d.status && d.status !== 'ok') notice = t(`fetches.test_${d.status}`);
      } else if (d?.ok) {
        notice = t('fetches.builderNoSample');
        showPaste = true;
      } else {
        err = d?.error ?? t('fetches.testNoAnswer');
        showPaste = true;
      }
    } catch {
      err = t('fetches.testNoAnswer');
      showPaste = true;
    }
    fetching = false;
  }

  function onPickPath(segs: string[]) {
    path = [...segs];
    pathPicked = true;
  }

  function useWholeResponse() {
    path = [];
    pathPicked = false;
  }

  async function create() {
    nameAttempted = true;
    urlAttempted = true;
    if (!slug || !url.trim()) {
      void focusFirstInvalid(buildEl);
      return;
    }
    err = '';
    creating = true;
    try {
      const r = await post('savefetch', draftForm());
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; defs?: SourceDef[]; error?: string }
        | undefined;
      if (d?.ok) {
        if (d.defs) onDefsChanged?.(d.defs);
        onInsert(tokenFor(slug));
        building = false;
      } else {
        err = d?.error ?? t('fetches.toastSaveFailed');
      }
    } catch {
      err = t('fetches.toastSaveFailed');
    }
    creating = false;
  }

  let armedDelete = $state('');

  async function remove(name: string) {
    if (armedDelete !== name) {
      armedDelete = name;
      panelErr = '';
      return;
    }
    armedDelete = '';
    panelErr = '';
    const f = new FormData();
    f.set('name', name);
    try {
      const r = await post('deletefetch', f);
      const d = (r.type === 'success' || r.type === 'failure' ? r.data : undefined) as
        | { ok?: boolean; defs?: SourceDef[]; error?: string }
        | undefined;
      if (d?.ok && d.defs) {
        onDefsChanged?.(d.defs);
        return;
      }
      panelErr = d?.error ?? t('fetches.toastDeleteFailed', { name });
    } catch {
      panelErr = t('fetches.toastDeleteFailed', { name });
    }
  }

  function removeFor(name: string) {
    const armed = armedDelete === name;
    return {
      label: armed ? t('fetches.deleteTitle', { name }) : t('fetches.deleteAria', { name }),
      armed,
      armedLabel: t('common.delete'),
      onclick: () => remove(name)
    };
  }
</script>

<div class="fsp">
  <Chip
    tone="muted"
    title={t('vars.urlfetch.hint')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={toggle}
    {@attach setAnchor}
  >
    {t('commandEditor.pickDataSource')}
    <Icon name="chevron" />
  </Chip>

  <PickerPanel
    {open}
    anchor={btnEl}
    label={t('fetches.pickerExistingTitle')}
    width={300}
    maxHeight={340}
    onClose={() => (open = false)}
  >
    {#snippet children()}
      <Heading level={6} as="p" variant="label">{t('fetches.pickerExistingTitle')}</Heading>
      {#if defs.length === 0}
        <Text size="xs" tone="muted">{t('fetches.builderNoneYet')}</Text>
      {:else}
        <ul class="opts">
          {#each defs.toSorted((a, b) => a.name.localeCompare(b.name)) as d (d.name)}
            <PickerOption
              as="li"
              layout="stacked"
              label={tokenFor(d.name)}
              description={d.json_path.length ? buildJsonPath(d.json_path) : t('fetches.kindPlain')}
              onclick={() => pick(d.name)}
              remove={removeFor(d.name)}
            />
          {/each}
        </ul>
      {/if}
      <FieldError message={panelErr} />
      {#if atQuota}
        <FieldError message={t('fetches.quotaReached', { max: String(DEFS_PER_BROADCASTER) })} />
      {:else}
        <Button variant="add" onclick={openBuilder}>{t('fetches.builderNew')}</Button>
      {/if}
    {/snippet}
  </PickerPanel>
</div>

<Modal open={building} title={t('fetches.builderTitle')} busy={creating} onClose={() => (building = false)}>
  <div class="build" bind:this={buildEl}>
    <Text size="sm" tone="muted">{t('fetches.builderIntro')}</Text>

    <Field label={t('fetches.displayName')}>
      <Input fill placeholder={t('fetches.displayNamePh')} bind:value={displayName} oninput={onDisplayName} />
    </Field>

    <Field label={t('fetches.slug')} hint={t('fetches.slugHint')} error={nameError} errorId="fetch-name-err">
      <Input
        fill
        mono
        invalid={!!nameError}
        required
        aria-invalid={nameError ? 'true' : undefined}
        aria-describedby={nameError ? 'fetch-name-err' : undefined}
        bind:value={slug}
        oninput={() => {
          slugTouched = true;
          slug = slugifyName(slug);
        }}
      />
    </Field>

    <Field label={t('fetches.builderUrl')} error={urlError} errorId="fetch-url-err">
      <Input
        fill
        mono
        invalid={!!urlError}
        type="url"
        placeholder={t('fetches.builderUrlPh')}
        spellcheck="false"
        required
        aria-invalid={urlError ? 'true' : undefined}
        aria-describedby={urlError ? 'fetch-url-err' : undefined}
        bind:value={url}
      />
    </Field>

    {#if keys.length > 0}
      <Field label={t('fetches.auth')}>
        <Select
          fill
          bind:value={keyLabel}
          options={[{ value: '', label: t('fetches.authNone') }, ...keys.toSorted((a, b) => a.label.localeCompare(b.label)).map((k) => ({ value: k.label, label: k.label }))]}
        />
      </Field>
    {/if}

    <div class="sample-row">
      <Button variant="secondary" busy={fetching} onclick={fetchSample}>
        {fetching ? t('fetches.builderFetching') : t('fetches.builderFetch')}
      </Button>
      {#if !showPaste && sample === ''}
        <Button variant="ghost" size="sm" onclick={() => (showPaste = true)}>{t('fetches.builderPasteInstead')}</Button>
      {/if}
    </div>

    {#if notice}<Text as="small" size="xs" tone="accent" role="status">{notice}</Text>{/if}
    <FieldError message={err} />

    {#if showPaste}
      <Textarea
        fill
        mono
        rows={4}
        spellcheck="false"
        placeholder={t('fetches.pickerPlaceholder')}
        aria-label={t('fetches.pickerSampleAria')}
        bind:value={sample}
      />
    {/if}

    {#if sample !== ''}
      <Text size="xs" tone="accent">{t('fetches.builderPickPrompt')}</Text>
      <JsonTree json={sample} onPick={onPickPath} leafTitle={(segs) => `${tokenFor(slug || 'name')} → ${buildJsonPath(segs)}`} />
      <div class="chosen">
        {#if pathPicked && path.length > 0}
          <Tag tone="bare">{t('fetches.builderPicked')}</Tag>
          <Code tone="success">{buildJsonPath(path)}</Code>
          <Button variant="ghost" size="sm" onclick={useWholeResponse}>{t('fetches.builderWholeResponse')}</Button>
        {:else}
          <Text as="span" size="xs" tone="muted">{t('fetches.builderWholeSelected')}</Text>
        {/if}
      </div>
    {/if}

    <div class="foot">
      <Button variant="ghost" onclick={() => (building = false)}>{t('common.cancel')}</Button>
      <Button variant="primary" busy={creating} onclick={create}>
        {creating ? t('fetches.builderCreating') : t('fetches.builderCreate')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .fsp { display: inline-flex; }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }

  .build { display: flex; flex-direction: column; gap: 12px; width: 100%; min-width: 0; --field-gap: 5px; --field-mb: 0; }

  .sample-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }

  .chosen { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }

  .foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 4px; }
</style>
