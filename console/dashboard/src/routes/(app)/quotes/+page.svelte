<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, ConfirmDialog, toast, getI18n } from '@bagel/shared';
  import type { QuoteView } from '$lib/server/quotes-store';

  let { data } = $props();
  const { t } = getI18n();

  // Local source of truth, reseeded when a fresh SSR load lands.
  // svelte-ignore state_referenced_locally
  let quotes = $state<QuoteView[]>(data.quotes ?? []);
  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let addPerm = $state<string>(data.addPerm ?? 'mod');
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      quotes = data.quotes ?? [];
      enabled = data.enabled ?? false;
      addPerm = data.addPerm ?? 'mod';
    }
  });

  // Newest number first: the most recently added quote sits at the top.
  const rows = $derived(quotes.toSorted((a, b) => b.number - a.number));

  const permOptions = [
    { value: 'mod', label: t('quotes.permMod') },
    { value: 'vip', label: t('quotes.permVip') },
    { value: 'sub', label: t('quotes.permSub') },
    { value: 'everyone', label: t('quotes.permEveryone') }
  ];

  type ActionResult = { ok?: boolean; error?: string; quote?: QuoteView; number?: number };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }
  function failed(payload: ActionResult | undefined, fallbackKey: string) {
    toast('err', payload?.error ?? t(fallbackKey));
  }

  function fmtDate(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString();
  }

  // --- Add -------------------------------------------------------------------
  let draft = $state('');
  let adding = $state(false);
  const MAX = 450;

  const addSubmit: SubmitFunction = () => {
    if (!draft.trim()) return;
    adding = true;
    return async ({ result }) => {
      adding = false;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok) {
        draft = '';
        toast('ok', t('quotes.toastAdded'));
        await invalidateAll();
        return;
      }
      failed(payload, 'quotes.toastAddFailed');
    };
  };

  // --- Master toggle (optimistic) --------------------------------------------
  const masterSubmit: SubmitFunction = () => {
    const was = enabled;
    enabled = !was;
    return async ({ result }) => {
      if (result.type !== 'success') {
        enabled = was;
        toast('err', t('quotes.toastToggleFailed'));
      }
    };
  };

  // --- Permission select (optimistic) ----------------------------------------
  let permForm = $state<HTMLFormElement | null>(null);
  const permSubmit: SubmitFunction = () => {
    const was = addPerm;
    return async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok) return;
      addPerm = was;
      failed(payload, 'quotes.toastPermFailed');
    };
  };
  function onPermChange(e: Event) {
    addPerm = (e.currentTarget as HTMLSelectElement).value;
    permForm?.requestSubmit();
  }

  // --- Delete (confirm) ------------------------------------------------------
  let deleteTarget = $state<QuoteView | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok) {
        if (target) {
          quotes = quotes.filter((q) => q.number !== target.number);
          toast('ok', t('quotes.toastDeleted'));
        }
        await invalidateAll();
        return;
      }
      failed(payload, 'quotes.toastDeleteFailed');
    };
  };
</script>

<section class="screen active">
  <PageHead eyebrow={t('quotes.eyebrow')} description={t('quotes.description')}>
    {t('quotes.titlePre')}<em>{t('quotes.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> {t('quotes.degraded')}</div>
  {/if}

  <div class="toolbar">
    <form method="POST" action="?/toggle" use:enhance={masterSubmit} class="master">
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <button class="toggle {enabled ? 'on' : ''}" type="submit" aria-label={t('quotes.botOn')}></button>
      <span class="master-text">
        <span class="master-label">{t('quotes.botOn')}</span>
        <span class="master-hint">{t('quotes.botOnHint')}</span>
      </span>
    </form>
    <div class="grow"></div>
    <form method="POST" action="?/perm" use:enhance={permSubmit} bind:this={permForm} class="perm">
      <label for="add-perm">{t('quotes.permLabel')}</label>
      <select id="add-perm" name="add_perm" value={addPerm} onchange={onPermChange}>
        {#each permOptions as opt (opt.value)}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    </form>
  </div>

  <Card style="padding:16px">
    <form method="POST" action="?/add" use:enhance={addSubmit} class="add">
      <input
        type="text"
        name="text"
        bind:value={draft}
        maxlength={MAX}
        placeholder={t('quotes.addPlaceholder')}
        aria-label={t('quotes.addPlaceholder')}
      />
      <button class="btn primary" type="submit" disabled={adding || !draft.trim()}>
        <Icon name="plus" size={14} /> {t('quotes.addBtn')}
      </button>
    </form>
    <p class="add-hint">{t('quotes.addHint')}</p>
  </Card>

  <Card style="padding:6px 0 0; margin-top:16px">
    <div class="list">
      {#each rows as q (q.number)}
        <div class="row">
          <span class="num">#{q.number}</span>
          <span class="text">{q.text}</span>
          <span class="date">{fmtDate(q.created_at)}</span>
          <button
            class="mini danger"
            type="button"
            aria-label={t('quotes.deleteAria')}
            onclick={() => (deleteTarget = q)}
          >
            <Icon name="trash" size={14} />
          </button>
        </div>
      {/each}
      {#if rows.length === 0}
        <div class="empty">
          <p class="empty-title">{t('quotes.emptyTitle')}</p>
          <p class="empty-sub">{t('quotes.emptySub')}</p>
        </div>
      {/if}
    </div>
  </Card>
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('quotes.deleteTitle')}
  body={t('quotes.deleteBody')}
  confirmLabel={t('quotes.del')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="number" value={deleteTarget?.number ?? ''} />
</form>

<style>
  .degraded {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: 8px;
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  .master { display: inline-flex; align-items: center; gap: 12px; }
  .master-text { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .master-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 13px; color: var(--bb-white); }
  .master-hint { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }

  .perm { display: inline-flex; align-items: center; gap: 8px; }
  .perm label { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .perm select {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    background: var(--bb-bg-1, #16130f);
    border: 1px solid var(--rule);
    border-radius: 7px;
    padding: 7px 10px;
  }

  .add { display: flex; gap: 10px; align-items: stretch; }
  .add input {
    flex: 1;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-white);
    background: var(--bb-bg-1, #16130f);
    border: 1px solid var(--rule);
    border-radius: 7px;
    padding: 10px 12px;
  }
  .add input:focus { outline: none; border-color: var(--rule-strong); }
  .add-hint { margin: 8px 2px 0; font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); }

  .list { display: flex; flex-direction: column; }
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--rule);
  }
  .row:last-child { border-bottom: none; }
  .num {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13px;
    color: var(--bb-tan);
    white-space: nowrap;
  }
  .text {
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-white);
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .date {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    border: 1px solid var(--rule);
    border-radius: 7px;
    background: transparent;
    color: var(--bb-muted);
    cursor: pointer;
  }
  .mini.danger:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.5); }

  .empty { padding: 34px 18px; text-align: center; color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; }
  .empty-title { font-family: var(--bb-font-display); font-weight: 700; font-size: 17px; color: var(--bb-white); margin: 0 0 6px; }
  .empty-sub { margin: 0 auto; max-width: 44ch; }

  @media (max-width: 760px) {
    .master-hint { display: none; }
    .row { grid-template-columns: auto minmax(0, 1fr) auto; }
    .date { display: none; }
  }
</style>
