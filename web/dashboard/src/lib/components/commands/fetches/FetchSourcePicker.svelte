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
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import { focusFirstInvalid } from '@bagel/ui/svelte/forms';
  import {
    getI18n,
    actionPayload,
    slugifyName,
    buildJsonPath,
    DEFS_PER_BROADCASTER
  } from '@bagel/kit';
  import JsonTree from './JsonTree.svelte';

  const { t } = getI18n();

  let {
    defs = [],
    keys = [],
    canManageKeys,
    onInsert,
    onDefsChanged
  }: {
    defs?: SourceDef[];
    keys?: { label: string }[];
    canManageKeys: boolean;
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

  const blankForm = () => ({
    displayName: '',
    slug: '',
    slugTouched: false,
    url: '',
    keyLabel: '',
    sample: '',
    path: [] as string[],
    pathPicked: false,
    notice: '',
    err: '',
    showPaste: false,
    nameAttempted: false,
    urlAttempted: false
  });

  let form = $state(blankForm());
  let fetching = $state(false);
  let creating = $state(false);

  type SampleReply = { ok?: boolean; sample?: string; status?: string; error?: string };
  type DefsReply = { ok?: boolean; defs?: SourceDef[]; error?: string };

  const atQuota = $derived(defs.length >= DEFS_PER_BROADCASTER);
  let buildEl = $state<HTMLDivElement | null>(null);
  const nameError = $derived(form.nameAttempted && !form.slug ? t('fetches.errNameRequired') : undefined);
  const urlError = $derived(form.urlAttempted && !form.url.trim() ? t('fetches.errUrlRequired') : undefined);

  function onDisplayName() {
    if (!form.slugTouched) form.slug = slugifyName(form.displayName);
  }

  function openBuilder() {
    form = blankForm();
    open = false;
    building = true;
  }

  function draftForm(): FormData {
    const f = new FormData();
    f.set('name', form.slug);
    f.set('url', form.url.trim());
    f.set('kind', form.pathPicked && form.path.length > 0 ? 'json' : 'plain');
    f.set('path', buildJsonPath(form.path));
    f.set('key_label', form.keyLabel);
    f.set('edit', '0');
    return f;
  }

  async function post<T>(action: string, body: FormData) {
    const res = await fetch(`/commands?/${action}`, { method: 'POST', body });
    return actionPayload<T>(deserialize(await res.text()));
  }

  function applySample(reply: SampleReply | undefined) {
    if (reply?.sample) {
      form.sample = reply.sample;
      form.showPaste = false;
      if (reply.status && reply.status !== 'ok') form.notice = t(`fetches.test_${reply.status}`);
      return;
    }
    form.showPaste = true;
    if (reply?.ok) form.notice = t('fetches.builderNoSample');
    else form.err = reply?.error ?? t('fetches.testNoAnswer');
  }

  async function fetchSample() {
    form.urlAttempted = true;
    if (!form.url.trim()) {
      void focusFirstInvalid(buildEl);
      return;
    }
    form.err = '';
    form.notice = '';
    fetching = true;
    try {
      const f = draftForm();
      f.set('kind', 'plain');
      f.set('path', '');
      applySample(await post<SampleReply>('testfetch', f));
    } catch {
      form.err = t('fetches.testNoAnswer');
      form.showPaste = true;
    }
    fetching = false;
  }

  function onPickPath(segs: string[]) {
    form.path = [...segs];
    form.pathPicked = true;
  }

  function useWholeResponse() {
    form.path = [];
    form.pathPicked = false;
  }

  async function create() {
    form.nameAttempted = true;
    form.urlAttempted = true;
    if (!form.slug || !form.url.trim()) {
      void focusFirstInvalid(buildEl);
      return;
    }
    form.err = '';
    creating = true;
    try {
      const reply = await post<DefsReply>('savefetch', draftForm());
      if (reply?.ok) {
        if (reply.defs) onDefsChanged?.(reply.defs);
        onInsert(tokenFor(form.slug));
        building = false;
      } else {
        form.err = reply?.error ?? t('fetches.toastSaveFailed');
      }
    } catch {
      form.err = t('fetches.toastSaveFailed');
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
      const reply = await post<DefsReply>('deletefetch', f);
      if (reply?.ok && reply.defs) {
        onDefsChanged?.(reply.defs);
        return;
      }
      panelErr = reply?.error ?? t('fetches.toastDeleteFailed', { name });
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
      <Input fill placeholder={t('fetches.displayNamePh')} bind:value={form.displayName} oninput={onDisplayName} />
    </Field>

    <Field label={t('fetches.slug')} hint={t('fetches.slugHint')} error={nameError} errorId="fetch-name-err">
      <Input
        fill
        mono
        invalid={!!nameError}
        required
        aria-invalid={nameError ? 'true' : undefined}
        aria-describedby={nameError ? 'fetch-name-err' : undefined}
        bind:value={form.slug}
        oninput={() => {
          form.slugTouched = true;
          form.slug = slugifyName(form.slug);
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
        bind:value={form.url}
      />
    </Field>

    <Field label={t('fetches.auth')}>
      {#if keys.length === 0}
        {#if canManageKeys}
          <Text size="xs" tone="muted">{t('fetches.authEmpty')} <TextLink variant="inline" href="/settings#api-keys" external>{t('fetches.authEmptyCta')}</TextLink></Text>
        {:else}
          <Text size="xs" tone="muted">{t('fetches.authEmptyDelegate')}</Text>
        {/if}
      {:else}
        <Select
          fill
          bind:value={form.keyLabel}
          options={[{ value: '', label: t('fetches.authNone') }, ...keys.toSorted((a, b) => a.label.localeCompare(b.label)).map((k) => ({ value: k.label, label: k.label }))]}
        />
      {/if}
    </Field>

    <div class="sample-row">
      <Button variant="secondary" busy={fetching} onclick={fetchSample}>
        {fetching ? t('fetches.builderFetching') : t('fetches.builderFetch')}
      </Button>
      {#if !form.showPaste && form.sample === ''}
        <Button variant="ghost" size="sm" onclick={() => (form.showPaste = true)}>{t('fetches.builderPasteInstead')}</Button>
      {/if}
    </div>

    {#if form.notice}<Text as="small" size="xs" tone="accent" role="status">{form.notice}</Text>{/if}
    <FieldError message={form.err} />

    {#if form.showPaste}
      <Textarea
        fill
        mono
        rows={4}
        spellcheck="false"
        placeholder={t('fetches.pickerPlaceholder')}
        aria-label={t('fetches.pickerSampleAria')}
        bind:value={form.sample}
      />
    {/if}

    {#if form.sample !== ''}
      <Text size="xs" tone="accent">{t('fetches.builderPickPrompt')}</Text>
      <JsonTree json={form.sample} onPick={onPickPath} leafTitle={(segs) => `${tokenFor(form.slug || 'name')} → ${buildJsonPath(segs)}`} />
      <div class="chosen">
        {#if form.pathPicked && form.path.length > 0}
          <Tag tone="bare">{t('fetches.builderPicked')}</Tag>
          <Code tone="success">{buildJsonPath(form.path)}</Code>
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
