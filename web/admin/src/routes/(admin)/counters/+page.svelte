<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/ui/svelte/EditorFooter.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { BotCounter } from '$lib/server/services';
  import StatePill from '$lib/components/StatePill.svelte';
  import {
    NEW_COUNTER,
    blankCounter,
    counterComplete,
    type CounterDraft
  } from '$lib/components/counters/counter-draft';
  import type { BotCountersBundle } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  let counters = $state<BotCounter[]>([]);
  let loaded = $state(false);
  let degraded = $state(false);
  $effect(() => {
    let alive = true;
    loaded = false;
    data.bundle.then((b: BotCountersBundle) => {
      if (!alive) return;
      counters = b.counters;
      degraded = b.degraded;
      loaded = true;
    });
    return () => {
      alive = false;
    };
  });

  const rows = $derived([...counters].sort((a, b) => a.name.localeCompare(b.name)));

  const inspector = createInspector<CounterDraft>();
  let draft = $state<CounterDraft | null>(null);
  let busy = $state(false);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW_COUNTER);
  const selected = $derived(
    creating ? null : (counters.find((c) => c.name === inspector.selectedId) ?? null)
  );
  const canSave = $derived(inspector.dirty && !!draft && counterComplete(draft));

  const discard = createDiscardGuard(
    () => inspector.dirty,
    () => {
      inspector.reset();
      draft = null;
    }
  );

  function draftOf(c: BotCounter): CounterDraft {
    return { name: c.name, value: c.value };
  }

  function openNew() {
    discard.guard(() => {
      const blank = blankCounter();
      inspector.open(NEW_COUNTER, blank);
      draft = { ...blank };
    });
  }

  function openCounter(c: BotCounter) {
    if (inspector.selectedId === c.name) {
      close();
      return;
    }
    discard.guard(() => {
      inspector.open(c.name, draftOf(c));
      draft = draftOf(c);
    });
  }

  function close() {
    discard.guard(() => {
      inspector.reset();
      draft = null;
    });
  }

  function upsert(snapshot: CounterDraft, wasCreating: boolean) {
    if (!wasCreating) {
      counters = counters.map((c) => (c.name === snapshot.name ? { ...c, value: snapshot.value } : c));
      return;
    }
    counters = [...counters, { name: snapshot.name, scope: 'bot', value: '0' }];
  }

  const saveSubmit: SubmitFunction = () => {
    const begun = inspector.beginSave();
    const wasCreating = creating;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload(result);
      const ok = result.type === 'success' && p?.ok === true;
      const applied = begun
        ? inspector.resolved(begun.requestId, { type: ok ? 'success' : 'error' })
        : false;
      if (!ok) {
        failed(p, t('admin.counters.saveFailed'));
        return;
      }
      if (begun) upsert(begun.snapshot, wasCreating);
      toast('ok', wasCreating ? t('admin.counters.created') : t('admin.counters.updated'));
      if (wasCreating && applied) {
        inspector.reset();
        draft = null;
      }
    };
  };

  let deleteTarget = $state<BotCounter | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    const target = deleteTarget;
    busy = true;
    return async ({ result }) => {
      busy = false;
      deleteTarget = null;
      const p = actionPayload(result);
      if (result.type === 'success' && p?.ok) {
        counters = counters.filter((c) => c.name !== target?.name);
        inspector.reset();
        draft = null;
        toast('ok', t('admin.counters.deleted'));
        return;
      }
      failed(p, t('admin.counters.deleteFailed'));
    };
  };
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.counters.eyebrow')} description={t('admin.counters.description')}>
    {t('admin.counters.titlePre')}<em>{t('admin.counters.titleEm')}</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>{t('admin.counters.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      {#if loaded}
        <Text as="span" size="xs" tone="muted" mono>
          {rows.length === 1
            ? t('admin.counters.countOne')
            : t('admin.counters.count', { n: String(rows.length) })}
        </Text>
      {:else}
        <Skeleton variant="pill" width="110px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" onclick={openNew}>{t('admin.counters.add')}</Button>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={inspector.isOpen} width="380px">
    <DeckList>
      {#if !loaded}
        <SkeletonStack rows={3} height="56px" />
      {:else if rows.length}
        <ul class="bb-list" aria-label={t('admin.counters.listLabel')}>
          {#each rows as c (c.name)}
            <li>
              <ManagementRow
                selected={inspector.selectedId === c.name}
                expanded={inspector.selectedId === c.name}
                controls="counter-inspector"
                onselect={() => openCounter(c)}
                title={c.name}
                meta={t('admin.counters.rowMeta', { scope: c.scope })}
              >
                {#snippet marks()}
                  <StatePill tone="neutral">{BigInt(c.value).toLocaleString()}</StatePill>
                {/snippet}
              </ManagementRow>
            </li>
          {/each}
        </ul>
      {:else}
        <EmptyState title={t('admin.counters.empty')} body={t('admin.counters.emptyBody')} />
      {/if}
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('admin.counters.newTitle') : (selected?.name ?? '')}
        controls="counter-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        {#key inspector.selectedId}
          <form
            class="editor"
            method="POST"
            action={creating ? '?/create' : '?/set'}
            use:enhance={saveSubmit}
          >
            <input type="hidden" name="name" value={draft.name} />
            {#if !creating}<input type="hidden" name="value" value={String(draft.value)} />{/if}

            <Scroller fill padding="18px" smooth>
              <Stack gap={4}>
                {#if creating}
                  <Field label={t('admin.counters.fieldName')}>
                    <Input
                      fill mono
                      type="text"
                      maxlength={64}
                      placeholder={t('admin.counters.fieldNamePlaceholder')}
                      bind:value={draft.name}
                    />
                  </Field>
                {:else}
                  <Stack gap={1}>
                    <Heading level={5} as="p" variant="title">{draft.name}</Heading>
                    <Text size="xs" tone="muted" mono>
                      {t('admin.counters.rowMeta', { scope: selected?.scope ?? 'bot' })}
                    </Text>
                  </Stack>
                {/if}

                {#if !creating}
                  <Field label={t('admin.counters.fieldValue')}>
                    <Input
                      fill mono
                      type="text"
                      inputmode="numeric"
                      bind:value={draft.value}
                    />
                  </Field>
                  <Text size="sm" tone="muted">{t('admin.counters.valueHint')}</Text>
                {/if}

                {#if selected}
                  <Stack as="section" gap={2} align="start">
                    <Heading level={3} variant="label">{t('admin.counters.dangerTitle')}</Heading>
                    <Button
                      disabled={busy}
                      onclick={() => (deleteTarget = selected)}
                      tone="danger"
                    >
                      {t('common.delete')}
                    </Button>
                  </Stack>
                {/if}
              </Stack>
            </Scroller>

            <EditorFooter
              status={inspector.status}
              dirty={inspector.dirty}
              {canSave}
              saveLabel={creating ? t('admin.counters.addCta') : t('common.save')}
              cancelLabel={t('common.cancel')}
              savingLabel={t('admin.saving')}
              savedLabel={t('admin.saved')}
              dirtyLabel={t('admin.unsaved')}
              errorLabel={t('admin.saveFailed')}
              onCancel={close}
            />
          </form>
        {/key}
      </InspectorSurface>
    {/if}
  </DeckLayout>
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('admin.counters.confirmDeleteTitle')}
  body={deleteTarget ? t('admin.counters.confirmDeleteBody', { name: deleteTarget.name }) : undefined}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  danger
  {busy}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('common.done')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
</style>
