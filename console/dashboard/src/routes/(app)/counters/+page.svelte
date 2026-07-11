<script lang="ts">
  import { enhance } from '$app/forms';
  import { goto, invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, ConfirmDialog, toast, getI18n, type CounterDef, type CounterScope } from '@bagel/shared';

  let { data } = $props();
  const { t } = getI18n();

  // Create form state.
  let newName = $state('');
  let newScope = $state<CounterScope>('channel');
  let creating = $state(false);

  // Per-row "set value" editor: one open at a time.
  let editing = $state<string | null>(null);
  let editValue = $state(0);

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    viewer_command: t('counters.tagViewerCommand')
  };

  type ActionResult = { ok?: boolean; error?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  const createSubmit: SubmitFunction = () => {
    creating = true;
    return async ({ result }) => {
      creating = false;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastCreated'));
        newName = '';
        newScope = 'channel';
        await invalidateAll();
        return;
      }
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  const setSubmit =
    (reset: boolean): SubmitFunction =>
    () => {
      return async ({ result }) => {
        if (result.type === 'success' && payloadOf(result)?.ok) {
          toast('ok', t(reset ? 'counters.toastReset' : 'counters.toastSet'));
          editing = null;
          await invalidateAll();
          return;
        }
        toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
      };
    };

  // Delete (confirm dialog).
  let deleteTarget = $state<CounterDef | null>(null);
  let deleting = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);
  const deleteSubmit: SubmitFunction = () => {
    deleting = true;
    return async ({ result }) => {
      deleting = false;
      const target = deleteTarget;
      deleteTarget = null;
      if (result.type === 'success' && payloadOf(result)?.ok) {
        toast('ok', t('counters.toastDeleted'));
        if (target && data.selected === target.name) await goto('/counters', { noScroll: true });
        await invalidateAll();
        return;
      }
      toast('err', payloadOf(result)?.error ?? t('counters.toastFailed'));
    };
  };

  function openSet(c: CounterDef) {
    if (editing === c.name) {
      editing = null;
      return;
    }
    editing = c.name;
    editValue = c.scope === 'channel' ? c.value : 0;
  }

  function toggleEntries(c: CounterDef) {
    const target = data.selected === c.name ? '/counters' : `/counters?c=${encodeURIComponent(c.name)}`;
    void goto(target, { noScroll: true, keepFocus: true });
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('counters.eyebrow')} description={t('counters.description')}>
    {t('counters.titlePre')}<em>{t('counters.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> {t('counters.degraded')}</div>
  {/if}

  <div class="grid">
    <div class="main">
      <Card style="padding:18px">
        <h3 class="card-title">{t('counters.newTitle')}</h3>
        <form method="POST" action="?/create" use:enhance={createSubmit} class="create" novalidate>
          <label class="field">
            <span>{t('counters.fieldName')}</span>
            <input class="search" name="name" placeholder={t('counters.fieldNamePh')} maxlength="64" required bind:value={newName} />
          </label>
          <label class="field">
            <span>{t('counters.fieldScope')}</span>
            <select class="search" name="scope" bind:value={newScope}>
              <option value="channel">{t('counters.scopeChannel')}</option>
              <option value="viewer">{t('counters.scopeViewer')}</option>
              <option value="viewer_command">{t('counters.scopeViewerCommand')}</option>
            </select>
          </label>
          <button type="submit" class="btn primary" disabled={creating || !newName.trim()}>
            <Icon name="plus" size={14} />
            {creating ? t('counters.creating') : t('counters.create')}
          </button>
        </form>
        <p class="hint">{t('counters.scopeHint')}</p>
      </Card>

      <Card style="padding:6px 0 0">
        <div class="list">
          {#each data.counters ?? [] as c (c.name)}
            <div class="row" class:selected={data.selected === c.name}>
              <div class="row-main">
                <span class="name">{c.name}</span>
                <span class="tag">{scopeTag[c.scope]}</span>
                {#if c.scope === 'channel'}
                  <span class="value">{c.value.toLocaleString()}</span>
                {/if}
              </div>
              <div class="row-actions">
                {#if c.scope !== 'channel'}
                  <button class="mini-btn" type="button" onclick={() => toggleEntries(c)}>
                    {data.selected === c.name ? t('counters.hideEntries') : t('counters.viewEntries')}
                  </button>
                {/if}
                <button class="mini-btn" type="button" onclick={() => openSet(c)}>
                  {c.scope === 'channel' ? t('counters.setValue') : t('counters.reset')}
                </button>
                <button class="mini-btn danger" type="button" aria-label={t('counters.del')} onclick={() => (deleteTarget = c)}>
                  <Icon name="x" size={12} />
                </button>
              </div>
              {#if editing === c.name}
                <form method="POST" action="?/set" use:enhance={setSubmit(c.scope !== 'channel')} class="set-form">
                  <input type="hidden" name="name" value={c.name} />
                  {#if c.scope === 'channel'}
                    <input class="search num" type="number" name="value" bind:value={editValue} />
                  {:else}
                    <input type="hidden" name="value" value="0" />
                    <span class="hint" style="margin:0">{t('counters.resetHint')}</span>
                  {/if}
                  <button type="submit" class="btn primary sm">{c.scope === 'channel' ? t('counters.set') : t('counters.reset')}</button>
                </form>
              {/if}
            </div>
          {/each}
          {#if (data.counters ?? []).length === 0}
            <div class="empty">
              <p class="empty-title">{t('counters.emptyTitle')}</p>
              <p class="empty-sub">{t('counters.emptySub')}</p>
            </div>
          {/if}
        </div>
      </Card>
    </div>

    {#if data.selected}
      <Card style="padding:18px">
        <h3 class="card-title">{t('counters.entriesTitle', { name: data.selected })}</h3>
        {#if (data.entries ?? []).length === 0}
          <p class="hint">{t('counters.entriesEmpty')}</p>
        {:else}
          <table class="tbl">
            <thead>
              <tr>
                <th>{t('counters.colViewer')}</th>
                <th>{t('counters.colSource')}</th>
                <th class="r">{t('counters.colValue')}</th>
              </tr>
            </thead>
            <tbody>
              {#each data.entries ?? [] as e (e.viewerId + ':' + e.command)}
                <tr>
                  <td>{e.viewerLogin || e.viewerId}</td>
                  <td class="mut">{e.command || '—'}</td>
                  <td class="r">{e.value.toLocaleString()}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </Card>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('counters.deleteTitle')}
  body={t('counters.deleteBody', { name: deleteTarget?.name ?? '' })}
  confirmLabel={t('counters.del')}
  cancelLabel={t('common.cancel')}
  danger
  busy={deleting}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
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

  .grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; }
  @media (min-width: 980px) {
    .grid { grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr); }
  }
  .main { display: flex; flex-direction: column; gap: 16px; }

  .card-title { margin: 0 0 10px; font-family: var(--bb-font-display); font-size: 15px; color: var(--bb-white); }
  .hint { margin: 10px 0 0; font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .create { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .create .field { flex: 1; min-width: 160px; display: flex; flex-direction: column; gap: 6px; }
  .create .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .create :global(.search) { width: 100%; box-sizing: border-box; }

  .list { display: flex; flex-direction: column; }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    border-bottom: 1px solid rgba(240, 236, 228, 0.05);
  }
  .row.selected { background: rgba(240, 236, 228, 0.03); }
  .row-main { display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0; }
  .name { font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tag {
    font-family: var(--bb-font-body);
    font-size: 11px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 2px 8px;
    white-space: nowrap;
  }
  .value { font-family: var(--bb-font-body); font-size: 14px; color: var(--bb-white); margin-left: auto; }
  .row-actions { display: flex; gap: 8px; align-items: center; }
  .mini-btn {
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
    background: transparent;
    border: 1px solid var(--bb-border);
    border-radius: 6px;
    padding: 4px 9px;
    cursor: pointer;
  }
  .mini-btn:hover { color: var(--bb-white); }
  .mini-btn.danger:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.5); }

  .set-form { display: flex; align-items: center; gap: 10px; width: 100%; padding-top: 4px; }
  .set-form .num { width: 140px; }
  .btn.sm { padding: 6px 12px; font-size: 12px; }

  .empty { padding: 28px 16px 34px; text-align: center; }
  .empty-title { margin: 0 0 4px; font-family: var(--bb-font-display); font-weight: 700; color: var(--bb-white); }
  .empty-sub { margin: 0; font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }

  .tbl { width: 100%; border-collapse: collapse; font-family: var(--bb-font-body); font-size: 13px; }
  .tbl th {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    padding: 4px 8px;
    border-bottom: 1px solid var(--bb-border);
  }
  .tbl td { padding: 7px 8px; border-bottom: 1px solid rgba(240, 236, 228, 0.05); color: var(--bb-white); }
  .tbl .r { text-align: right; }
  .tbl .mut { color: var(--bb-muted); }

  @media (max-width: 480px) {
    .create .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
