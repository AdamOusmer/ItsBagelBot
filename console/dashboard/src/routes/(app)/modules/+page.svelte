<script lang="ts">
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Card, PageHead, Scroller, toast, getI18n, type ModuleState } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import ModuleRow from '$lib/components/modules/ModuleRow.svelte';
  import ModuleEditor from '$lib/components/modules/ModuleEditor.svelte';

  let { data } = $props();

  const { t } = getI18n();

  // Local source of truth, seeded from the SSR load (mirrors the commands page).
  // svelte-ignore state_referenced_locally
  let items = $state<ModuleState[]>(data.modules ?? []);

  // svelte-ignore state_referenced_locally
  let seed = data.modules;
  $effect(() => {
    if (data.modules !== seed) {
      seed = data.modules;
      items = data.modules ?? [];
    }
  });

  const activeCount = $derived(items.filter((m) => m.enabled).length);

  // --- Per-row save-state machine (same shape as the commands page) ----------
  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(id: string, s: SaveState) {
    for (const tm of timers.get(id) ?? []) clearTimeout(tm);
    timers.delete(id);
    modStatus = { ...modStatus, [id]: s };
  }
  function ackSaved(id: string) {
    setStatus(id, 'saved');
    timers.set(id, [
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'live' }), 2500),
      setTimeout(() => (modStatus = { ...modStatus, [id]: 'idle' }), 7000)
    ]);
  }
  function flagError(id: string) {
    setStatus(id, 'error');
    timers.set(id, [setTimeout(() => (modStatus = { ...modStatus, [id]: 'idle' }), 4000)]);
  }

  // --- List quick-toggle (optimistic flip with rollback) ---------------------
  const toggleSubmit =
    (m: ModuleState): SubmitFunction =>
    () => {
      const wasEnabled = m.enabled;
      items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: !wasEnabled } : x));
      // Keep an open inspector's draft in sync with the quick toggle.
      if (expanded === m.def.id) draftEnabled = !wasEnabled;
      setStatus(m.def.id, 'saving');
      return async ({ result }) => {
        const payload =
          result.type === 'success' || result.type === 'failure'
            ? (result.data as { ok?: boolean } | undefined)
            : undefined;
        if (result.type === 'success' && payload?.ok) {
          ackSaved(m.def.id);
        } else {
          items = items.map((x) => (x.def.id === m.def.id ? { ...x, enabled: wasEnabled } : x));
          if (expanded === m.def.id) draftEnabled = wasEnabled;
          flagError(m.def.id);
          toast('err', t('modules.couldNotToggle', { label: m.def.label }));
        }
      };
    };

  // --- Docked inspector (config editor) --------------------------------------
  let expanded = $state<string | null>(null);
  let draftEnabled = $state(false);
  let draftConfig = $state<Record<string, string>>({});
  let busy = $state(false);

  const selected = $derived(expanded ? items.find((m) => m.def.id === expanded) : undefined);

  function openConfig(m: ModuleState) {
    if (expanded === m.def.id) {
      closeConfig();
      return;
    }
    draftEnabled = m.enabled;
    draftConfig = { ...m.config };
    expanded = m.def.id;
  }

  function closeConfig() {
    expanded = null;
  }

  // --- Save (optimistic apply of the whole draft, with rollback) -------------
  const saveSubmit: SubmitFunction = () => {
    const m = selected;
    if (!m) return;
    const id = m.def.id;
    const snapshot = { ...m, config: { ...m.config } };
    const nextEnabled = draftEnabled;
    const nextConfig = { ...draftConfig };

    items = items.map((x) => (x.def.id === id ? { ...x, enabled: nextEnabled, config: nextConfig } : x));
    busy = true;
    setStatus(id, 'saving');

    return async ({ result }) => {
      busy = false;
      const payload =
        result.type === 'success' || result.type === 'failure'
          ? (result.data as { ok?: boolean } | undefined)
          : undefined;
      if (result.type === 'success' && payload?.ok) {
        ackSaved(id);
        closeConfig();
        toast('ok', t('modules.saved', { label: m.def.label }));
      } else {
        items = items.map((x) => (x.def.id === id ? snapshot : x));
        flagError(id);
        toast('err', t('modules.saveFailed'));
      }
    };
  };

  // Escape closes the inspector, unless focus is in a field.
  function isTyping(e: KeyboardEvent): boolean {
    const el = e.target as HTMLElement | null;
    return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && expanded && !isTyping(e)) closeConfig();
  }
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('modules.eyebrow')}
    description={t('modules.description', { active: activeCount, total: items.length })}
  >{t('modules.titlePre')}<em>{t('modules.titleEm')}</em></PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert">
      <Icon name="ban" size={13} />
      {t('modules.degraded')}
    </div>
  {/if}

  <!-- The deck: ledger list left, docked inspector right — same as commands. -->
  <div class="deck {expanded ? 'inspecting' : ''}">
    <Card style="padding:6px 0 0" class="deck-list">
      <div class="list">
        {#each items as m, i (m.def.id)}
          <ModuleRow
            module={m}
            index={i + 1}
            status={modStatus[m.def.id] ?? 'idle'}
            expanded={expanded === m.def.id}
            onExpand={() => openConfig(m)}
            toggleSubmit={toggleSubmit(m)}
          />
        {/each}
        {#if items.length === 0}
          <div class="empty">{t('modules.empty')}</div>
        {/if}
      </div>
    </Card>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={!!expanded}
      role="presentation"
      onclick={closeConfig}
      onkeydown={(e) => { if (e.key === 'Enter') closeConfig(); }}
    ></div>
    <aside class="inspector" class:open={!!expanded} aria-label="Module inspector">
      <div class="inspector-head">
        <span class="inspector-tag">
          {#if selected}{t('modules.configuring', { label: selected.def.label })}{:else}{t('modules.inspector')}{/if}
        </span>
        {#if selected}
          <button class="mini" type="button" aria-label={t('modules.closeEditor')} onclick={closeConfig}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>
      {#if selected}
        <Scroller fill padding="16px" data-lenis-prevent>
          <!-- Keyed on the module so switching rows mounts a fresh editor with
               its own seeded draft (mirrors the commands inspector). -->
          {#key selected.def.id}
            <ModuleEditor
              module={selected}
              bind:enabled={draftEnabled}
              bind:config={draftConfig}
              {busy}
              onCancel={closeConfig}
              onSubmit={saveSubmit}
            />
          {/key}
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="modules" size={18} /></span>
          <p>{t('modules.inspectorIdle')}</p>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<style>
  .degraded {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  /* ── the deck: list + docked inspector (identical to the commands deck) ── */
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
    .deck { grid-template-columns: minmax(0, 1fr) 300px; }
  }

  .list :global(.row-shell:last-child) { border-bottom: none; }

  .inspector {
    position: sticky;
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: var(--bb-radius-lg);
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 62px - 108px);
  }
  .inspector-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--rule);
  }
  .inspector-tag {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
  }

  .inspector-idle {
    padding: 34px 20px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .idle-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    border: 1px solid var(--rule-tan);
    border-radius: var(--bb-radius-sm);
    color: var(--bb-tan-light);
  }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .inspector-backdrop { display: none; }

  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0;
      top: auto;
      z-index: 220;
      max-height: 88vh;
      border-radius: var(--bb-radius-lg) var(--bb-radius-lg) 0 0;
      background: var(--bb-bg-1, #111);
      animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) both;
    }
    .inspector-backdrop.open {
      display: block;
      position: fixed; inset: 0; z-index: 219;
      background: rgba(0, 0, 0, 0.55);
    }
    @keyframes sheet-in { from { transform: translateY(100%); } to { transform: translateY(0); } }
  }

  .empty {
    padding: 34px 18px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
  }
</style>
