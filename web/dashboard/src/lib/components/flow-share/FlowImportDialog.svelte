<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit';
  import { MAX_FLOW_FILE_BYTES } from '@bagel/kit/automations/limits';
  import type { FlowImportEntryView, FlowImportResultView, FlowLanding, Issue, SetupNote } from '@bagel/kit/automations';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import FileDrop from '@bagel/ui/svelte/FileDrop.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Spinner from '@bagel/ui/svelte/Spinner.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { postAction } from '#lib/client-actions.svelte';
  import type { FlowImportCommitReply, FlowImportPreviewReply } from '#lib/flow-share';

  let { path = '', onImported, startOpen = false }: { path?: string; onImported?: () => void; startOpen?: boolean } = $props();

  const { t } = getI18n();

  let open = $state(untrack(() => startOpen));
  let busy = $state(false);
  let file = $state<File | null>(null);
  let problem = $state<string[]>([]);
  let entries = $state<FlowImportEntryView[]>([]);
  let results = $state<FlowImportResultView[]>([]);
  let slots = $state(0);
  let selected = $state(new Set<number>());

  const stage = $derived(results.length > 0 ? 'done' : entries.length > 0 ? 'preview' : 'choose');
  const ready = $derived(entries.filter((entry) => entry.status === 'ready'));
  const needSetup = $derived(ready.filter((entry) => entry.setup.length > 0).length);
  const blocked = $derived(entries.length - ready.length);
  const created = $derived(results.filter((result) => result.status === 'created').length);

  function fileIssue(issue: Issue): string {
    switch (issue.code) {
      case 'file_too_large':
        return t('automations.share.issue.file_too_large');
      case 'invalid_json':
        return t('automations.share.issue.invalid_json');
      case 'invalid_format':
        return t('automations.share.issue.invalid_format');
      case 'unknown_format':
        return t('automations.share.issue.unknown_format');
      case 'unsupported_version':
        return t('automations.share.issue.unsupported_version');
      case 'unknown_field':
        return t('automations.share.issue.unknown_field');
      case 'too_many':
        return t('automations.share.issue.too_many');
      case 'required':
        return t('automations.share.issue.required');
      default:
        return t('automations.share.issue.generic', { code: issue.code });
    }
  }

  function recipeIssue(issue: Issue): string {
    if (issue.code === 'uses_token') return t('automations.share.issue.uses_token', { count: '{count}', uses: '{uses}' });
    if (issue.code === 'write_failed') return t('automations.share.issue.write_failed');
    if (issue.code === 'command_exists') return t('automations.share.issue.command_exists');
    if (issue.code === 'alert_exists') return t('automations.share.issue.alert_exists');
    return t('automations.share.issue.generic', { code: `${issue.path} ${issue.code}`.trim() });
  }

  function setupText(note: SetupNote): string {
    switch (note.code) {
      case 'reward_trigger':
        return t('automations.share.setup.reward_trigger');
      case 'event_filter':
        return t('automations.share.setup.event_filter');
      case 'users_list':
        return t('automations.share.setup.users_list');
      case 'login_target':
        return t('automations.share.setup.login_target');
      case 'http_target':
        return t('automations.share.setup.http_target');
      case 'lights_target':
        return t('automations.share.setup.lights_target');
      case 'slash_command':
        return t('automations.share.setup.slash_command');
      case 'run_removed':
        return t('automations.share.setup.run_removed');
      case 'run_target_simple':
        return t('automations.share.setup.run_target_simple');
      default:
        return t('automations.share.setup.link_failed');
    }
  }

  function reviewText(note: SetupNote): string {
    const detail = note.detail ?? '';
    switch (note.code) {
      case 'run_linked':
        return t('automations.share.review.run_linked', { detail });
      case 'counter':
        return t('automations.share.review.counter', { detail });
      case 'fetch':
        return t('automations.share.review.fetch', { detail });
      case 'command_in_use':
        return t('automations.share.review.command_in_use', { detail });
      default:
        return t('automations.share.review.privileged_action', { detail });
    }
  }

  function statusText(status: FlowImportEntryView['status'] | FlowImportResultView['status']): string {
    switch (status) {
      case 'ready':
        return t('automations.share.statusReady');
      case 'invalid':
        return t('automations.share.statusInvalid');
      case 'conflict':
        return t('automations.share.statusConflict');
      case 'over_limit':
        return t('automations.share.statusOverLimit');
      case 'skipped':
        return t('automations.share.statusSkipped');
      case 'created':
        return t('automations.share.statusCreated');
      default:
        return t('automations.share.statusFailed');
    }
  }

  function landingText(landing: FlowLanding | undefined, target: string | undefined): string {
    if (landing === 'command') return t('automations.share.landingCommand', { name: target ?? '' });
    if (landing === 'alert') return t('automations.share.landingAlert', { name: target ?? '' });
    return landing === 'automation' ? t('automations.share.landingAutomation') : '';
  }

  const uniqueTexts = (texts: string[]) => [...new Set(texts)];

  function reset() {
    file = null;
    problem = [];
    entries = [];
    results = [];
    selected = new Set();
    busy = false;
  }

  function close() {
    open = false;
    reset();
  }

  function refusal(reply: { error?: string; issues?: Issue[] } | null): string[] {
    if (reply?.issues?.length) return uniqueTexts(reply.issues.map(fileIssue));
    return [reply?.error ?? t('automations.share.importError')];
  }

  async function choose(next: File | undefined) {
    if (!next || busy) return;
    reset();
    file = next;
    if (next.size > MAX_FLOW_FILE_BYTES) {
      problem = [t('automations.share.issue.file_too_large')];
      return;
    }
    busy = true;
    const body = new FormData();
    body.set('file', next);
    const reply = await postAction<FlowImportPreviewReply>('flowImportPreview', body, path);
    busy = false;
    if (!reply?.ok || !reply.entries) {
      problem = refusal(reply);
      return;
    }
    entries = reply.entries;
    slots = reply.slots ?? 0;
    selected = new Set(reply.entries.filter((entry) => entry.status === 'ready').map((entry) => entry.index));
  }

  function toggle(index: number, on: boolean) {
    const next = new Set(selected);
    if (on) next.add(index);
    else next.delete(index);
    selected = next;
  }

  async function commit() {
    if (!file || selected.size === 0 || busy) return;
    busy = true;
    const body = new FormData();
    body.set('file', file);
    for (const index of selected) body.append('index', String(index));
    const reply = await postAction<FlowImportCommitReply>('flowImportCommit', body, path);
    busy = false;
    if (!reply?.ok || !reply.results) {
      problem = refusal(reply);
      return;
    }
    results = reply.results;
    onImported?.();
  }
</script>

{#snippet notes(setup: SetupNote[], review: SetupNote[], issues: Issue[])}
  {#if issues.length > 0}
    <div class="group">
      <span class="heading">{t('automations.share.issuesHeading')}</span>
      <ul>{#each uniqueTexts(issues.map(recipeIssue)) as text (text)}<li>{text}</li>{/each}</ul>
    </div>
  {/if}
  {#if setup.length > 0}
    <div class="group">
      <span class="heading">{t('automations.share.setupHeading')}</span>
      <ul>{#each uniqueTexts(setup.map(setupText)) as text (text)}<li>{text}</li>{/each}</ul>
    </div>
  {/if}
  {#if review.length > 0}
    <div class="group">
      <span class="heading">{t('automations.share.reviewHeading')}</span>
      <ul>{#each uniqueTexts(review.map(reviewText)) as text (text)}<li>{text}</li>{/each}</ul>
    </div>
  {/if}
{/snippet}

{#snippet landed(landing: FlowLanding | undefined, target: string | undefined)}
  {#if landingText(landing, target)}<span class="sub">{landingText(landing, target)}</span>{/if}
{/snippet}

{#snippet title(name: string, sourceName: string, renamed: boolean, status: FlowImportEntryView['status'] | FlowImportResultView['status'])}
  <span class="title">
    <span class="name">{name || t('automations.share.unnamed')}</span>
    <Badge status tone={status === 'ready' || status === 'created' ? 'success' : status === 'skipped' ? undefined : 'danger'}>{statusText(status)}</Badge>
  </span>
  {#if renamed}<span class="sub">{t('automations.share.renamedFrom', { name: sourceName })}</span>{/if}
{/snippet}

<Button variant="secondary" onclick={() => (open = true)}>{t('automations.share.importOpen')}</Button>

<Modal bind:open title={t('automations.share.importTitle')} onClose={close} {busy}>
  <div class="body">
    {#if stage === 'choose'}
      <p>{t('automations.share.importBody')}</p>
      <FileDrop label={t('automations.share.importDrop')} accept="application/json,.json" {file} onFileChange={choose} />
      <div class="state" aria-live="polite">
        {#if busy}<Spinner /> <span>{t('automations.share.importChecking')}</span>{/if}
        {#each problem as text (text)}<Text as="p" tone="danger">{text}</Text>{/each}
      </div>
      <div class="foot">
        <Button variant="ghost" disabled={busy} onclick={close}>{t('common.cancel')}</Button>
      </div>
    {:else if stage === 'preview'}
      <p>{t('automations.share.importCount', { ready: ready.length, setup: needSetup, blocked })}</p>
      <p>{t('automations.share.importSlots', { n: slots })}</p>
      <ul class="list">
        {#each entries as entry (entry.index)}
          <li class="row">
            {#if entry.status === 'ready'}
              <Checkbox checked={selected.has(entry.index)} onchange={(event) => toggle(entry.index, event.currentTarget.checked)}>
                {@render title(entry.name, entry.sourceName, entry.renamed, entry.status)}
              </Checkbox>
            {:else}
              {@render title(entry.name, entry.sourceName, entry.renamed, entry.status)}
            {/if}
            {#if entry.status === 'ready'}{@render landed(entry.landing, entry.target)}{/if}
            {@render notes(entry.setup, entry.review, entry.issues)}
          </li>
        {/each}
      </ul>
      {#each problem as text (text)}<Text as="p" tone="danger">{text}</Text>{/each}
      <div class="foot">
        <Button variant="ghost" disabled={busy} onclick={reset}>{t('automations.share.importBack')}</Button>
        <Button {busy} disabled={selected.size === 0} onclick={commit}>{t('automations.share.importCta', { n: selected.size })}</Button>
      </div>
    {:else}
      <p>{t('automations.share.importDoneCount', { created, failed: results.filter((result) => result.status === 'failed' || result.status === 'invalid' || result.status === 'conflict' || result.status === 'over_limit').length })}</p>
      <ul class="list">
        {#each results as result (result.index)}
          <li class="row">
            {@render title(result.name, result.sourceName, result.renamed, result.status)}
            {#if result.status === 'created'}{@render landed(result.landing, result.target)}{/if}
            {@render notes(result.setup, result.review, result.issues)}
          </li>
        {/each}
      </ul>
      <div class="foot">
        <Button onclick={close}>{t('automations.share.importClose')}</Button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  p {
    margin: 0;
  }
  .state {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    min-height: 48px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-height: min(420px, 50vh);
    overflow-y: auto;
  }
  .row {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .title {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .sub {
    display: block;
    font-size: 13px;
    overflow-wrap: anywhere;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 14px;
  }
  .group ul {
    margin: 0;
    padding-left: 18px;
  }
  .heading {
    font-weight: 600;
  }
  .foot {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
