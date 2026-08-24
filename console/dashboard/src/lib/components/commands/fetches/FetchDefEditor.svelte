<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Fetch-definition editor: structured builder first (display name -> slug,
  // URL, plain/json kind with the path picker, optional stored key), a raw
  // source view over the SAME rehearsal template for power users, and the
  // server-side dry-run ("Test run") whose values merge into ChatPreview so
  // the author sees exactly what chat would render — fan-out, truncation,
  // slash-verb routing and all.
  //
  // Progressive enhancement: Save is a real <form method="POST" action="?/save">
  // wrapped in use:enhance; Test run posts through the page's postAction
  // helper (deserialize) so it works from the same action surface without a
  // full reload.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    AlertBanner,
    Button,
    EditorFooter,
    FieldError,
    Scroller,
    DEFS_PER_BROADCASTER,
    RESPONSE_MAX_LINES,
    getI18n,
    malformedUrlFetchTokens,
    slugifyName,
    validateFetchDef,
    type FetchDefErrors,
    type Samples
  } from '@bagel/shared';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import ResponseEditor from '$lib/components/commands/ResponseEditor.svelte';
  import ChatPreview from '$lib/components/commands/ChatPreview.svelte';
  import FetchPathPicker from './FetchPathPicker.svelte';
  import { saveFetchDraft, type FetchDraft } from './drafts';

  const { t } = getI18n();

  let {
    draft = $bindable<FetchDraft>(),
    defs = [] as { name: string }[],
    keys = [] as { label: string }[],
    status = 'idle' as 'idle' | 'saving' | 'saved' | 'error' | 'conflict',
    dirty = false,
    testing = false,
    testError = '',
    testStatus = '' as string,
    testSamples = undefined as Samples | undefined,
    onCancel,
    onSubmit,
    onTest
  }: {
    draft: FetchDraft;
    /** Saved definitions — collision + quota checks run client-side too. */
    defs?: { name: string }[];
    /** Stored key labels for the auth picker. */
    keys?: { label: string }[];
    status?: 'idle' | 'saving' | 'saved' | 'error' | 'conflict';
    dirty?: boolean;
    testing?: boolean;
    testError?: string;
    /** Last dry-run verdict ('' = none yet); non-ok statuses banner here. */
    testStatus?: string;
    /** Merged rehearsal samples after a successful dry-run. */
    testSamples?: Samples;
    onCancel: () => void;
    onSubmit: SubmitFunction;
    onTest: () => void;
  } = $props();

  const busy = $derived(status === 'saving');

  // The builder/source segmented control. One draft underneath: Source shows
  // the template verbatim (tokens unexpanded), Builder structures the def.
  const VIEWS = ['builder', 'source'] as const;
  let view = $state<(typeof VIEWS)[number]>('builder');

  // Auto-slug from the display name until the slug itself is edited by hand;
  // then the two decouple (an author may want `wx` from "Weather").
  let slugTouched = $state(draft.edit);
  function onDisplayName() {
    if (!slugTouched) draft.name = slugifyName(draft.displayName);
  }
  function onSlugInput() {
    slugTouched = true;
    draft.name = slugifyName(draft.name);
  }

  const errors = $derived.by<FetchDefErrors>(() => {
    const e = validateFetchDef({
      name: draft.name,
      url: draft.url,
      kind: draft.kind,
      path: draft.path,
      keyLabel: draft.key_label
    });
    if (!e.name && draft.name && defs.some((d) => d.name === draft.name && d.name !== draft.originalName)) {
      e.name = t('fetches.errNameTaken', { name: draft.name });
    }
    return e;
  });

  const quotaReached = $derived(!draft.edit && defs.length >= DEFS_PER_BROADCASTER);
  const canSave = $derived(!quotaReached && Object.keys(errors).length === 0);

  // Malformed `{urlfetch…` spans flagged in the source view (mark.unknown
  // treatment): typos stay visible, matching the engine's leave-unknown-
  // tokens-literal rule.
  const badTokens = $derived(malformedUrlFetchTokens(draft.template));

  // Mirror the working draft to sessionStorage (CommandEditor precedent):
  // navigation/refresh can't eat work in progress.
  $effect(() => {
    saveFetchDraft(draft);
  });

  // Client-side gate in front of the form action: only sound fields reach the
  // server (which re-runs the same shared validator authoritatively).
  const submit: SubmitFunction = (input) => {
    if (Object.keys(errors).length || quotaReached) {
      input.cancel();
      return;
    }
    return onSubmit(input);
  };

  const kindOptions = [
    { value: 'plain', label: t('fetches.kindPlain') },
    { value: 'json', label: t('fetches.kindJson') }
  ] as const;

  function setKind(k: string) {
    draft.kind = k === 'json' ? 'json' : 'plain';
    if (draft.kind === 'plain') draft.path = [];
  }

  function removeSegment(i: number) {
    draft.path.splice(i, 1);
  }</script>

<form method="POST" action="?/save" class="editor-form" novalidate use:enhance={submit}>
  <input type="hidden" name="name" value={draft.name} />
  <input type="hidden" name="kind" value={draft.kind} />
  <input type="hidden" name="path" value={draft.path.join('.')} />
  {#if draft.edit}
    <input type="hidden" name="edit" value="1" />
    <input type="hidden" name="original_name" value={draft.originalName} />
  {/if}

  <Scroller fill padding="16px" data-lenis-prevent>
    <div class="editor">
      {#if quotaReached}
        <AlertBanner icon="caps">{t('fetches.quotaReached', { max: String(DEFS_PER_BROADCASTER) })}</AlertBanner>
      {/if}

      <div class="view-toggle" role="radiogroup" aria-label={t('fetches.viewToggle')}>
        {#each VIEWS as v (v)}
          <button
            type="button"
            class="seg-chip"
            class:on={view === v}
            role="radio"
            aria-checked={view === v}
            onclick={() => (view = v)}
          >{v === 'builder' ? t('fetches.viewBuilder') : t('fetches.viewSource')}</button>
        {/each}
      </div>

      {#if view === 'builder'}
        <label class="field">
          <span>{t('fetches.displayName')}</span>
          <input
            class="search"
            placeholder={t('fetches.displayNamePh')}
            bind:value={draft.displayName}
            oninput={onDisplayName}
          />
        </label>

        <label class="field">
          <span>{t('fetches.slug')} <small>{draft.edit ? t('common.optional') : ''}</small></span>
          <input class="search mono" bind:value={draft.name} oninput={onSlugInput} maxlength={32} required />
          <FieldError message={errors.name} />
          <small class="hint">{t('fetches.slugHint')}</small>
        </label>

        <label class="field">
          <span>{t('fetches.url')}</span>
          <input
            class="search mono"
            placeholder="https://api.example.com/v1/…"
            spellcheck="false"
            autocomplete="off"
            bind:value={draft.url}
            required
          />
          <FieldError message={errors.url} />
        </label>

        <div class="field">
          <span>{t('fetches.kind')}</span>
          <div class="kind-pills" role="radiogroup" aria-label={t('fetches.kind')}>
            {#each kindOptions as opt (opt.value)}
              <label class="pill" class:on={draft.kind === opt.value}>
                <input
                  type="radio"
                  name="kind_display"
                  value={opt.value}
                  checked={draft.kind === opt.value}
                  onchange={() => setKind(opt.value)}
                />
                <span class="dot" aria-hidden="true"></span>
                {opt.label}
              </label>
            {/each}
          </div>
          <FieldError message={errors.kind} />
        </div>

        {#if draft.kind === 'json'}
          <div class="field">
            <span>{t('fetches.path')} <small>{t('common.optional')}</small></span>
            {#if draft.path.length > 0}
              <div class="path-chips">
                {#each draft.path as seg, i (i)}
                  <span class="path-seg">
                    {seg}
                    <button type="button" aria-label={t('fetches.pathRemove', { seg })} onclick={() => removeSegment(i)}>×</button>
                  </span>
                  {#if i < draft.path.length - 1}<span class="path-dot">.</span>{/if}
                {/each}
              </div>
            {/if}
            <FetchPathPicker
              name={draft.name}
              mode="defpath"
              onPickPath={(segs) => (draft.path = [...segs])}
              onInsert={(token) => (draft.template += token)}
            />
            <FieldError message={errors.path} />
          </div>
        {/if}

        <label class="field">
          <span>{t('fetches.auth')} <small>{t('common.optional')}</small></span>
          <select class="search" bind:value={draft.key_label}>
            <option value="">{t('fetches.authNone')}</option>
            {#each keys.toSorted((a, b) => a.label.localeCompare(b.label)) as k (k.label)}
              <option value={k.label}>{k.label}</option>
            {/each}
          </select>
          <FieldError message={errors.key_label} />
          <small class="hint">{t('fetches.authHint')}</small>
        </label>
      {:else}
        <!-- Source view: the same template, tokens verbatim. Def fields render
             read-only above so the raw text stays honest about what ships. -->
        <div class="src-meta">
          <code>{`{urlfetch:${draft.name}}`}</code>
          <span>{draft.kind === 'plain' ? t('fetches.kindPlain') : `${t('fetches.kindJson')} · ${draft.path.join('.') || '/'}`}</span>
          <span class="truncate">{draft.url}</span>
        </div>
        {#each badTokens as bad, i (String(i) + bad)}
          <small class="bad-token" role="alert">{t('fetches.badToken')}: <code>{bad}</code></small>
        {/each}
      {/if}

      <div class="field">
        <span>{t('fetches.template')}</span>
        {#if view === 'builder'}
          <!-- The palette carries the {urlfetch:…} chip and the path picker, so
               picked leaves insert at the caret through the editor's own
               insert() (the CounterPicker path). -->
          <ResponseEditor bind:value={draft.template} maxLines={RESPONSE_MAX_LINES} fetchPickerName={draft.name} />
        {:else}
          <textarea
            class="raw mono"
            rows="4"
            placeholder={t('commandEditor.responsePlaceholder')}
            bind:value={draft.template}
          ></textarea>
        {/if}
        <small class="hint">{t('fetches.templateHint')}</small>
      </div>

      <ChatPreview name={draft.name || 'name'} response={draft.template} samples={testSamples} />

      <div class="test-row">
        <Button variant="secondary" icon="send" loading={testing} onclick={onTest}>
          {t('fetches.testRun')}
        </Button>
        {#if testStatus === 'ok'}
          <small class="ok">{t('fetches.testOk')}</small>
        {:else if testStatus !== ''}
          <small class="warn">{t(`fetches.test_${testStatus}`)}</small>
        {/if}
      </div>
      {#if testError}
        <AlertBanner icon="ban">{testError}</AlertBanner>
      {/if}

      <div class="check">
        <CheckButton name="is_active" bind:checked={draft.is_active} label={t('fetches.active')} />
      </div>
    </div>
  </Scroller>

  <EditorFooter
    {status}
    {dirty}
    canSave={canSave}
    saveLabel={draft.edit ? t('commandEditor.saveChanges') : t('fetches.create')}
    cancelLabel={t('common.cancel')}
    savingLabel={t('commandEditor.saving')}
    savedLabel={t('commands.saved')}
    errorLabel={t('commands.toastSaveFailed')}
    dirtyLabel={t('commands.unsavedChanges')}
    {onCancel}
  />
</form>

<style>
  .editor-form { display: flex; flex-direction: column; min-height: 0; flex: 1; }
  .editor { padding: 4px 2px 2px; }

  .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }

  .mono { font-family: var(--bb-font-mono); font-size: 12.5px; }
  .hint { color: var(--bb-muted); opacity: 0.7; font-size: 11px; font-family: var(--bb-font-body); line-height: 1.5; }

  /* Response-kind pills: real radio inputs so the choice survives no-JS posts
     through the hidden kind field too. */
  .kind-pills { display: flex; gap: 10px; flex-wrap: wrap; }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: 0.04em;
    padding: 9px 14px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--glass-border);
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-muted);
    cursor: pointer;
    user-select: none;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .pill:hover { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .pill.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }
  .pill input { position: absolute; opacity: 0; width: 0; height: 0; }
  .pill .dot {
    flex: 0 0 auto;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 1px solid var(--glass-border);
    background: rgba(0, 0, 0, 0.22);
  }
  .pill.on .dot {
    border-color: var(--bb-tan-light, #e0c9a4);
    background: var(--bb-tan, #c9a87c);
    box-shadow: 0 0 0 3px rgba(201, 168, 124, 0.18);
  }
  .pill input:focus-visible ~ .dot { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  .view-toggle { display: inline-flex; gap: 6px; margin-bottom: 16px; }
  .seg-chip {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-muted);
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill);
    padding: 5px 14px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) ease;
  }
  .seg-chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }

  .path-chips { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
  .path-seg {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-white);
    background: rgba(82, 183, 136, 0.08);
    border: 1px solid rgba(82, 183, 136, 0.25);
    border-radius: 999px;
    padding: 3px 10px;
  }
  .path-seg button {
    border: none;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
    font-size: 12px;
    line-height: 1;
    padding: 0;
  }
  .path-seg button:hover { color: #cf8a78; }
  .path-dot { font-family: var(--bb-font-mono); color: var(--bb-muted); }

  .src-meta {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }
  .src-meta code { color: var(--bb-green-glow, #52b788); }
  .src-meta .truncate { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .bad-token {
    display: block;
    margin-bottom: 8px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: #cf8a78;
  }
  .bad-token code { font-family: var(--bb-font-mono); }

  .raw {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 96px;
    padding: 12px 14px 26px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: 8px 8px;
    color: var(--bb-white);
    font-size: 13px;
    line-height: 1.6;
  }
  .raw::placeholder { color: var(--bb-muted); opacity: 0.7; }
  .raw:focus { outline: none; border-color: rgba(82, 183, 136, 0.5); }

  .test-row { display: flex; align-items: center; gap: 12px; margin: 4px 0 12px; flex-wrap: wrap; }
  .test-row small { font-family: var(--bb-font-body); font-size: 11.5px; }
  .test-row .ok { color: var(--bb-green-glow, #52b788); }
  .test-row .warn { color: var(--bb-tan-light, #c8a050); }

  .check { margin: 4px 0 14px; }
</style>
