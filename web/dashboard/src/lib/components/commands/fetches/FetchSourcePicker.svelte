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
  import { Button, Code, Field, Input, Modal, getI18n, slugifyName, buildJsonPath, DEFS_PER_BROADCASTER } from '@bagel/kit';
  import { PickerPanel } from '@bagel/kit';
  import { focusFirstInvalid } from '@bagel/kit';
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
  let btnEl = $state<HTMLButtonElement>();
  let panelErr = $state('');

  function toggle() {
    open = !open;
    if (!open) return;
    armedDelete = '';
    panelErr = '';
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
</script>

<div class="fsp">
  <button
    type="button"
    class="picker bb-chip bb-chip--muted"
    title={t('vars.urlfetch.hint')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={toggle}
    bind:this={btnEl}
  >
    {t('commandEditor.pickDataSource')}
    <span class="caret" aria-hidden="true">▾</span>
  </button>

  <PickerPanel
    {open}
    anchor={btnEl}
    label={t('fetches.pickerExistingTitle')}
    width={300}
    maxHeight={340}
    onClose={() => (open = false)}
  >
    {#snippet children()}
      <p class="panel-title">{t('fetches.pickerExistingTitle')}</p>
      {#if defs.length === 0}
        <p class="mut">{t('fetches.builderNoneYet')}</p>
      {:else}
        <ul class="opts">
          {#each defs.toSorted((a, b) => a.name.localeCompare(b.name)) as d (d.name)}
            <li>
              <button type="button" class="opt" onclick={() => pick(d.name)}>
                <span class="opt-name">{tokenFor(d.name)}</span>
                <span class="opt-path">{d.json_path.length ? buildJsonPath(d.json_path) : t('fetches.kindPlain')}</span>
              </button>
              <button
                type="button"
                class="opt-del"
                class:armed={armedDelete === d.name}
                aria-label={armedDelete === d.name
                  ? t('fetches.deleteTitle', { name: d.name })
                  : t('fetches.deleteAria', { name: d.name })}
                onclick={() => remove(d.name)}>{armedDelete === d.name ? t('common.delete') : '×'}</button
              >
            </li>
          {/each}
        </ul>
      {/if}
      {#if panelErr}
        <small class="err" role="alert">{panelErr}</small>
      {/if}
      {#if atQuota}
        <small class="err">{t('fetches.quotaReached', { max: String(DEFS_PER_BROADCASTER) })}</small>
      {:else}
        <button type="button" class="new" onclick={openBuilder}>{t('fetches.builderNew')}</button>
      {/if}
    {/snippet}
  </PickerPanel>
</div>

<Modal open={building} title={t('fetches.builderTitle')} busy={creating} closeModal={() => (building = false)}>
  <div class="build" bind:this={buildEl}>
    <p class="intro">{t('fetches.builderIntro')}</p>

    <Field label={t('fetches.displayName')}>
      <input class="in" placeholder={t('fetches.displayNamePh')} bind:value={displayName} oninput={onDisplayName} />
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
        <select class="in" bind:value={keyLabel}>
          <option value="">{t('fetches.authNone')}</option>
          {#each keys.toSorted((a, b) => a.label.localeCompare(b.label)) as k (k.label)}
            <option value={k.label}>{k.label}</option>
          {/each}
        </select>
      </Field>
    {/if}

    <div class="sample-row">
      <Button variant="secondary" loading={fetching} onclick={fetchSample}>
        {fetching ? t('fetches.builderFetching') : t('fetches.builderFetch')}
      </Button>
      {#if !showPaste && sample === ''}
        <button type="button" class="link" onclick={() => (showPaste = true)}>{t('fetches.builderPasteInstead')}</button>
      {/if}
    </div>

    {#if notice}<small class="notice" role="status">{notice}</small>{/if}
    {#if err}<small class="err" role="alert">{err}</small>{/if}

    {#if showPaste}
      <textarea
        class="in mono paste"
        rows="4"
        spellcheck="false"
        placeholder={t('fetches.pickerPlaceholder')}
        aria-label={t('fetches.pickerSampleAria')}
        bind:value={sample}
      ></textarea>
    {/if}

    {#if sample !== ''}
      <p class="pick-prompt">{t('fetches.builderPickPrompt')}</p>
      <JsonTree json={sample} onPick={onPickPath} leafTitle={(segs) => `${tokenFor(slug || 'name')} → ${buildJsonPath(segs)}`} />
      <div class="chosen">
        {#if pathPicked && path.length > 0}
          <span class="chosen-tag bb-tag bb-tag--bare">{t('fetches.builderPicked')}</span>
          <Code class="chosen-path">{buildJsonPath(path)}</Code>
          <button type="button" class="link" onclick={useWholeResponse}>{t('fetches.builderWholeResponse')}</button>
        {:else}
          <span class="mut">{t('fetches.builderWholeSelected')}</span>
        {/if}
      </div>
    {/if}

    <div class="foot">
      <Button variant="ghost" onclick={() => (building = false)}>{t('common.cancel')}</Button>
      <Button variant="primary" loading={creating} onclick={create}>
        {creating ? t('fetches.builderCreating') : t('fetches.builderCreate')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .fsp { position: relative; display: inline-flex; }

  .picker { gap: 5px; font-family: var(--bb-font-body); color: var(--bb-muted); }
  .picker:hover,
  .picker[aria-expanded='true'] {
    color: var(--bb-white);
    border-color: var(--bb-border-strong, rgba(255, 255, 255, 0.24));
    background: rgba(255, 255, 255, 0.04);
  }
  .caret { font-size: 9px; opacity: 0.7; }


  .panel-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }

  .mut { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  p.mut { margin: 0; font-style: italic; }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .opts li { display: flex; align-items: center; gap: 4px; }
  .opt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: var(--bb-radius-sm);
    cursor: pointer;
    text-align: left;
  }
  .opt:hover { background: var(--glass-fill-2); }
  .opt-name { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-white); }
  .opt-path {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .opt-del {
    flex: none;
    min-width: 22px;
    height: 22px;
    padding: 0 6px;
    border: none;
    border-radius: var(--bb-radius-sm);
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
  }
  .opt-del:hover { color: var(--bb-status-error, #cf8a78); background: rgba(207, 138, 120, 0.12); }
  .opt-del.armed {
    color: var(--bb-status-error, #cf8a78);
    background: rgba(207, 138, 120, 0.16);
    font-family: var(--bb-font-body);
    font-size: 11px;
  }

  .new {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-green-glow, #52b788);
    background: rgba(82, 183, 136, 0.06);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: var(--bb-radius-pill);
    padding: 5px 12px;
    cursor: pointer;
  }
  .new:hover { background: rgba(82, 183, 136, 0.14); }

  .build { display: flex; flex-direction: column; gap: 12px; width: 100%; min-width: 0; }
  .intro { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; line-height: 1.55; color: var(--bb-muted); }

  .build { --field-gap: 5px; --field-mb: 0; }
  .in {
    width: 100%;
    box-sizing: border-box;
    padding: 9px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
  .in.mono { font-family: var(--bb-font-mono); font-size: 12px; }
  .in:focus { outline: none; border-color: rgba(82, 183, 136, 0.5); }
  .paste { resize: vertical; min-height: 74px; line-height: 1.5; }

  .sample-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .link {
    background: none;
    border: none;
    padding: 0;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
    text-decoration: underline;
    cursor: pointer;
  }
  .link:hover { color: var(--bb-white); }

  .notice { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-tan-light); }
  .err { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-status-error, #cf8a78); }

  .pick-prompt { margin: 0; font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-tan-light); }
  .chosen { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
  .chosen-tag { flex: none; }
  :global(.chosen-path) { color: var(--bb-green-glow, #52b788); min-width: 0; }

  .foot { display: flex; justify-content: flex-end; gap: 8px; padding-top: 4px; }
</style>
