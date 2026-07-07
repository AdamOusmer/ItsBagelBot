<script lang="ts">
  import { deserialize } from '$app/forms';
  import { Icon, Card, PageHead, Scroller, SaveStatus, toast, getI18n, automodToggleDefault, type ModuleField, type ModuleReply } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import ReplyRow from '$lib/components/modules/ReplyRow.svelte';
  import ReplyEditor from '$lib/components/modules/ReplyEditor.svelte';

  let { data } = $props();

  const { t } = getI18n();
  const def = $derived(data.def);

  // Draft: module enable + the flat config map. Seeded from the load and reseeded
  // when navigating to a different module (component reuse across [id] routes).
  // svelte-ignore state_referenced_locally
  let enabled = $state(data.enabled);
  // svelte-ignore state_referenced_locally
  let config = $state<Record<string, string>>({ ...data.config });
  // svelte-ignore state_referenced_locally
  let seedId = data.def.id;
  $effect(() => {
    if (data.def.id !== seedId) {
      seedId = data.def.id;
      enabled = data.enabled;
      config = { ...data.config };
      expanded = null;
    }
  });

  // --- Per-target save-state machine (module enable + each reply row) ---------
  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(key: string, s: SaveState) {
    for (const tm of timers.get(key) ?? []) clearTimeout(tm);
    timers.delete(key);
    modStatus = { ...modStatus, [key]: s };
  }
  function ackSaved(key: string) {
    setStatus(key, 'saved');
    timers.set(key, [
      setTimeout(() => (modStatus = { ...modStatus, [key]: 'live' }), 2500),
      setTimeout(() => (modStatus = { ...modStatus, [key]: 'idle' }), 7000)
    ]);
  }
  function flagError(key: string) {
    setStatus(key, 'error');
    timers.set(key, [setTimeout(() => (modStatus = { ...modStatus, [key]: 'idle' }), 4000)]);
  }

  // The whole config is one blob, so every write posts the full current draft.
  function buildBody(en: boolean, cfg: Record<string, string>): FormData {
    const body = new FormData();
    body.set('is_enabled', en ? 'on' : '');
    for (const reply of def.replies) {
      body.set(`cfg.${reply.messageKey}`, cfg[reply.messageKey] ?? '');
      if (reply.enableKey) body.set(`cfg.${reply.enableKey}`, cfg[reply.enableKey] === 'off' ? 'off' : 'on');
    }
    for (const field of def.settings ?? []) body.set(`cfg.${field.key}`, cfg[field.key] ?? '');
    return body;
  }

  async function persist(en: boolean, cfg: Record<string, string>): Promise<boolean> {
    const res = await fetch('?/save', { method: 'POST', body: buildBody(en, cfg) }).catch(() => null);
    if (!res) return false;
    const result = deserialize(await res.text());
    const payload =
      result.type === 'success' || result.type === 'failure'
        ? (result.data as { ok?: boolean } | undefined)
        : undefined;
    return !!(result.type === 'success' && payload?.ok);
  }

  // --- Module master toggle (optimistic) -------------------------------------
  async function toggleModule() {
    const before = enabled;
    enabled = !enabled;
    setStatus('module', 'saving');
    if (await persist(enabled, config)) ackSaved('module');
    else {
      enabled = before;
      flagError('module');
      toast('err', t('modules.couldNotToggle', { label: def.label }));
    }
  }

  // --- Plain settings fields (linked account, ...) ----------------------------
  async function saveSetting(field: ModuleField, value: string) {
    const key = field.key;
    const before = config[key] ?? '';
    if (value.trim() === before.trim()) return;
    config = { ...config, [key]: value.trim() };
    setStatus(`setting:${key}`, 'saving');
    if (await persist(enabled, config)) ackSaved(`setting:${key}`);
    else {
      config = { ...config, [key]: before };
      flagError(`setting:${key}`);
      toast('err', t('modules.saveFailed'));
    }
  }

  // A follows-level toggle setting rests on its level's default (see
  // automodToggleDefault) until the user flips it, when the blob stores an
  // explicit "on"/"off". Mirrors the Go tri-state in app/sesame/automod/config.go.
  function settingToggleOn(field: ModuleField): boolean {
    const v = config[field.key] ?? '';
    if (v === 'on') return true;
    if (v === 'off') return false;
    return field.followsLevel ? automodToggleDefault(config['level'] || 'moderate', field.key) : false;
  }

  // --- Per-reply toggle (optimistic) -----------------------------------------
  async function toggleReply(reply: ModuleReply) {
    if (!reply.enableKey) return;
    const key = reply.enableKey;
    const was = config[key] !== 'off';
    config = { ...config, [key]: was ? 'off' : 'on' };
    setStatus(reply.key, 'saving');
    if (await persist(enabled, config)) ackSaved(reply.key);
    else {
      config = { ...config, [key]: was ? 'on' : 'off' };
      flagError(reply.key);
      toast('err', t('modules.couldNotToggle', { label: reply.label }));
    }
  }

  // --- Reply builder inspector ------------------------------------------------
  let expanded = $state<string | null>(null);
  let editMessage = $state('');
  let busy = $state(false);
  const selectedReply = $derived(expanded ? def.replies.find((r) => r.key === expanded) : undefined);

  function openReply(reply: ModuleReply) {
    if (expanded === reply.key) {
      closeInspector();
      return;
    }
    editMessage = config[reply.messageKey] ?? '';
    expanded = reply.key;
  }
  function closeInspector() {
    expanded = null;
  }

  async function saveReply() {
    const r = selectedReply;
    if (!r) return;
    const prev = config[r.messageKey];
    config = { ...config, [r.messageKey]: editMessage };
    busy = true;
    setStatus(r.key, 'saving');
    const ok = await persist(enabled, config);
    busy = false;
    if (ok) {
      ackSaved(r.key);
      closeInspector();
      toast('ok', t('modules.saved', { label: def.label }));
    } else {
      config = { ...config, [r.messageKey]: prev ?? '' };
      flagError(r.key);
      toast('err', t('modules.saveFailed'));
    }
  }

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !expanded) return;
    const el = e.target as HTMLElement | null;
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return;
    closeInspector();
  }
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> {t('modules.allModules')}</a>
  <PageHead eyebrow={t('modules.detailEyebrow')} description={def.description}>{def.label}</PageHead>

  {#if data.degraded}
    <div class="degraded" role="alert"><Icon name="ban" size={13} /> {t('modules.degraded')}</div>
  {/if}

  <!-- Settings strip: the module master switch (and any non-reply settings). -->
  <Card style="padding:0" class="settings-card">
    <div class="toggle-row">
      <div class="tr-text">
        <span class="tr-label">{t('modules.enabled')}</span>
        <span class="tr-help">{t('modules.enabledHelp')}</span>
      </div>
      <SaveStatus state={modStatus['module'] ?? 'idle'} />
      <button
        class="toggle {enabled ? 'on' : ''}"
        type="button"
        aria-label={t('modules.toggleAria', { label: def.label })}
        onclick={toggleModule}
      ></button>
    </div>
    {#each def.settings ?? [] as field (field.key)}
      <div class="setting-row">
        <label class="tr-text" for="mod-setting-{field.key}">
          <span class="tr-label">{field.label}</span>
          {#if field.help}<span class="tr-help">{field.help}</span>{/if}
        </label>
        <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
        {#if field.type === 'toggle'}
          <button
            id="mod-setting-{field.key}"
            class="toggle {settingToggleOn(field) ? 'on' : ''}"
            type="button"
            aria-label="Toggle {field.label}"
            onclick={() => saveSetting(field, settingToggleOn(field) ? 'off' : 'on')}
          ></button>
        {:else if field.type === 'select'}
          <select
            id="mod-setting-{field.key}"
            class="setting-input"
            value={config[field.key] || field.placeholder || field.options?.[0]?.value || ''}
            onchange={(e) => saveSetting(field, e.currentTarget.value)}
          >
            {#each field.options ?? [] as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        {:else}
          <input
            id="mod-setting-{field.key}"
            class="setting-input"
            type={field.type === 'number' ? 'number' : 'text'}
            placeholder={field.placeholder ?? ''}
            value={config[field.key] ?? ''}
            onchange={(e) => saveSetting(field, e.currentTarget.value)}
            onkeydown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); }}
          />
        {/if}
      </div>
    {/each}
  </Card>

  <!-- The deck: reply ledger + docked builder inspector (same as commands). -->
  <div class="deck {expanded ? 'inspecting' : ''} {enabled ? '' : 'muted'}">
    <Card style="padding:6px 0 0" class="deck-list">
      <div class="list">
        {#each def.replies as reply, i (reply.key)}
          <ReplyRow
            {reply}
            message={config[reply.messageKey] ?? ''}
            index={i + 1}
            status={modStatus[reply.key] ?? 'idle'}
            expanded={expanded === reply.key}
            enabled={reply.enableKey ? config[reply.enableKey] !== 'off' : undefined}
            onExpand={() => openReply(reply)}
            onToggle={() => toggleReply(reply)}
          />
        {/each}
      </div>
    </Card>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={!!expanded}
      role="presentation"
      onclick={closeInspector}
      onkeydown={(e) => { if (e.key === 'Enter') closeInspector(); }}
    ></div>
    <aside class="inspector" class:open={!!expanded} aria-label="Reply builder">
      <div class="inspector-head">
        <span class="inspector-tag">{selectedReply ? selectedReply.label : t('modules.inspector')}</span>
        {#if selectedReply}
          <button class="mini" type="button" aria-label={t('modules.closeEditor')} onclick={closeInspector}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>
      {#if selectedReply}
        <Scroller fill padding="16px" data-lenis-prevent>
          {#key selectedReply.key}
            <ReplyEditor reply={selectedReply} bind:message={editMessage} {busy} onCancel={closeInspector} onSave={saveReply} />
          {/key}
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="modules" size={18} /></span>
          <p>{t('modules.replyIdle')}</p>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    text-decoration: none;
    margin-bottom: 10px;
  }
  .back:hover { color: var(--bb-white); }

  .degraded {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid rgba(176, 90, 70, 0.4);
    border-radius: 8px 8px;
    background: rgba(176, 90, 70, 0.08);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13px;
  }

  :global(.settings-card) { margin-bottom: 16px; }
  .toggle-row { display: flex; align-items: center; gap: 12px; padding: 16px 18px; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; }
  .tr-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  /* Plain settings fields under the master switch (e.g. linked account). */
  .setting-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    border-top: 1px solid var(--rule);
  }
  .setting-input {
    width: min(260px, 44vw);
    padding: 8px 12px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    transition: border-color var(--bb-dur-fast, 140ms) ease;
  }
  .setting-input:focus {
    outline: none;
    border-color: var(--bb-tan, #c9a87c);
  }
  .setting-input::placeholder { color: var(--bb-muted); opacity: 0.7; }
  @media (max-width: 560px) {
    .setting-row { flex-wrap: wrap; }
    .setting-input { width: 100%; }
  }

  /* ── the deck: list + docked inspector (identical to the commands deck) ── */
  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
    transition: opacity var(--bb-dur-fast, 140ms) ease;
  }
  .deck.muted { opacity: 0.72; }
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
    border-radius: 8px;
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
    border-radius: 8px;
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
      border-radius: 8px 8px 0 0;
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
</style>
