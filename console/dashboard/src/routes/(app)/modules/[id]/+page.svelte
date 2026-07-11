<script lang="ts">
  import { deserialize } from '$app/forms';
  import { Icon, Card, PageHead, Scroller, SaveStatus, Switch, InspectorSurface, ConfirmDialog, AlertBanner, DeckList, EmptyState, toast, getI18n, automodToggleDefault, type ModuleField, type ModuleReply } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
  import ReplyRow from '$lib/components/modules/ReplyRow.svelte';
  import ReplyEditor from '$lib/components/modules/ReplyEditor.svelte';
  import ModuleCommandRow from '$lib/components/modules/ModuleCommandRow.svelte';
  import TriggerRuleEditor from '$lib/components/modules/TriggerRuleEditor.svelte';

  let { data } = $props();

  const { t } = getI18n();
  const def = $derived(data.def);
  // A module with no editable replies (its lines are fixed system text, e.g. the
  // play queue) shows only its read-only command list — no builder inspector.
  const hasReplies = $derived(def.replies.length > 0);

  // Draft: module enable + the flat config map. Seeded from the load and reseeded
  // when navigating to a different module (component reuse across [id] routes).
  // svelte-ignore state_referenced_locally
  let enabled = $state(data.enabled);
  // svelte-ignore state_referenced_locally
  let config = $state<Record<string, string>>({ ...data.config });

  // Trigger words is the one module whose "replies" are a free-form list the
  // author grows: it renders trigger rules as add/removable ReplyRows and reads
  // the whole list out of one "rules" config string (see the trigger block
  // below). Every other module keeps its fixed def.replies.
  const isTriggers = $derived(def.id === 'triggers');
  // svelte-ignore state_referenced_locally
  let rules = $state<Rule[]>(parseRules(data.config.rules ?? ''));
  // The inspector column exists for any module that has an editable ledger —
  // fixed replies or the dynamic trigger list.
  const hasInspector = $derived(isTriggers || hasReplies);
  // Whether there is any deck at all (replies, triggers, or a read-only command
  // list). A settings-only module (AutoMod) has none, so it renders no empty deck.
  const hasDeck = $derived(hasInspector || (def.commands?.length ?? 0) > 0);

  // svelte-ignore state_referenced_locally
  let seedId = data.def.id;
  $effect(() => {
    if (data.def.id !== seedId) {
      seedId = data.def.id;
      enabled = data.enabled;
      config = { ...data.config };
      rules = parseRules(data.config.rules ?? '');
      ruleIndex = null;
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
    // saved -> idle only: no timer-driven "live"/synced claim (no delivery ack).
    setStatus(key, 'saved');
    timers.set(key, [setTimeout(() => (modStatus = { ...modStatus, [key]: 'idle' }), 3000)]);
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
    // Triggers persists its whole rule list as one config string.
    if (def.id === 'triggers') body.set('cfg.rules', cfg.rules ?? '');
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

  // Dirty guard: close / row-switch / add all route through one confirmation so
  // an in-progress reply or trigger edit is never silently dropped.
  let discardOpen = $state(false);
  let afterDiscard: (() => void) | null = null;
  function guarded(action: () => void) {
    if (inspectorDirty) {
      afterDiscard = action;
      discardOpen = true;
    } else {
      action();
    }
  }
  function confirmDiscard() {
    discardOpen = false;
    const a = afterDiscard;
    afterDiscard = null;
    a?.();
  }
  function cancelDiscard() {
    discardOpen = false;
    afterDiscard = null;
  }
  // Unguarded close (after a save or delete, when there is nothing to lose).
  function doClose() {
    expanded = null;
    ruleIndex = null;
  }

  function openReply(reply: ModuleReply) {
    if (expanded === reply.key) {
      closeInspector();
      return;
    }
    guarded(() => {
      editMessage = config[reply.messageKey] ?? '';
      expanded = reply.key;
    });
  }
  function closeInspector() {
    guarded(doClose);
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
      // Save keeps the inspector open on the saved reply (now clean); no close.
      toast('ok', t('modules.saved', { label: def.label }));
    } else {
      config = { ...config, [r.messageKey]: prev ?? '' };
      flagError(r.key);
      toast('err', t('modules.saveFailed'));
    }
  }

  // --- Trigger words: dynamic rule list ---------------------------------------
  // A rule is one "phrase => response" line. The list persists as one config
  // string (config.rules); a disabled rule is stored as a "#" comment the sesame
  // parser skips. parse/serialize mirror app/sesame/modules/triggers.go.
  type Match = 'word' | 'contains' | 'exact' | 'prefix';
  type Rule = { phrase: string; response: string; match: Match; enabled: boolean };

  const MODE_LABEL: Record<Match, string> = {
    word: 'Whole word',
    contains: 'Contains',
    exact: 'Exact message',
    prefix: 'Starts with'
  };

  function splitMode(left: string): [Match, string] {
    const c = left.indexOf(':');
    if (c < 0) return ['word', left];
    const pre = left.slice(0, c).trim().toLowerCase();
    if (pre === 'word' || pre === 'contains' || pre === 'exact' || pre === 'prefix') return [pre, left.slice(c + 1).trim()];
    return ['word', left];
  }
  function parseRules(raw: string): Rule[] {
    const out: Rule[] = [];
    for (const line of raw.split('\n')) {
      let ln = line.trim();
      if (!ln) continue;
      let on = true;
      if (ln.startsWith('#')) {
        const rest = ln.slice(1).trim();
        if (!rest.includes('=>')) continue; // a plain comment, not a disabled rule
        on = false;
        ln = rest;
      }
      const sep = ln.indexOf('=>');
      if (sep < 0) continue;
      const [match, phrase] = splitMode(ln.slice(0, sep).trim());
      const response = ln.slice(sep + 2).trim();
      if (!phrase || !response) continue;
      out.push({ phrase, response, match, enabled: on });
    }
    return out;
  }
  function serializeRules(list: Rule[]): string {
    return list
      .filter((r) => r.phrase.trim() && r.response.trim())
      .map((r) => {
        const mode = r.match === 'word' ? '' : r.match + ': ';
        const body = `${mode}${r.phrase.trim()} => ${r.response.replace(/\s*\n\s*/g, ' ').trim()}`;
        return r.enabled ? body : `# ${body}`;
      })
      .join('\n');
  }

  // Rules rendered as ReplyRow-shaped rows (label = phrase, preview = response).
  const ruleRows: ModuleReply[] = $derived(
    rules.map((r, i) => ({
      key: `rule:${i}`,
      label: r.phrase || 'New rule',
      tagline: MODE_LABEL[r.match],
      event: `on "${r.phrase || '…'}"`,
      messageKey: `rule:${i}`,
      enableKey: `rule:${i}`,
      defaultMessage: r.response
    }))
  );

  // Inspector draft. ruleIndex is the row being edited, -1 for a new unsaved rule,
  // or null when no rule is open. The response reuses editMessage (shared with the
  // reply editor's bound message).
  let ruleIndex = $state<number | null>(null);
  let draftPhrase = $state('');
  let draftMatch = $state<Match>('word');

  // persistRules writes the serialized list into config.rules and posts the whole
  // draft, so the module enable and the rules save together. config is committed
  // only on success (the caller commits `rules` too).
  async function persistRules(next: Rule[]): Promise<boolean> {
    const cfg = { ...config, rules: serializeRules(next) };
    const ok = await persist(enabled, cfg);
    if (ok) config = cfg;
    return ok;
  }

  function openRule(i: number) {
    if (expanded === `rule:${i}`) return closeInspector();
    guarded(() => {
      const r = rules[i];
      ruleIndex = i;
      draftPhrase = r.phrase;
      draftMatch = r.match;
      editMessage = r.response;
      expanded = `rule:${i}`;
    });
  }
  function addRule() {
    guarded(() => {
      ruleIndex = -1;
      draftPhrase = '';
      draftMatch = 'word';
      editMessage = '';
      expanded = 'rule:new';
    });
  }

  // Reject phrases that would corrupt the "[mode:] phrase => response" wire
  // format on the round trip (a late structured-record migration removes this
  // limit). Blocks the exact vectors: an embedded "=>", a leading "#" (parsed as
  // a disabled rule), and a reserved mode: prefix.
  function reservedPhrase(phrase: string): boolean {
    return phrase.includes('=>') || phrase.startsWith('#') || /^(word|contains|exact|prefix)\s*:/i.test(phrase);
  }

  async function saveRule() {
    if (ruleIndex === null) return;
    if (reservedPhrase(draftPhrase.trim())) {
      toast('err', t('modules.triggerReserved'));
      return;
    }
    const keepOn = ruleIndex === -1 ? true : (rules[ruleIndex]?.enabled ?? true);
    const draft: Rule = { phrase: draftPhrase.trim(), response: editMessage, match: draftMatch, enabled: keepOn };
    const next = ruleIndex === -1 ? [...rules, draft] : rules.map((r, i) => (i === ruleIndex ? draft : r));
    const key = expanded ?? 'rule';
    busy = true;
    setStatus(key, 'saving');
    const ok = await persistRules(next);
    busy = false;
    if (ok) {
      rules = next;
      // Keep the inspector open on the saved rule (a new rule becomes the last
      // row); it now reads clean.
      if (ruleIndex === -1) {
        ruleIndex = next.length - 1;
        expanded = `rule:${next.length - 1}`;
      }
      toast('ok', t('modules.saved', { label: def.label }));
    } else {
      flagError(key);
      toast('err', t('modules.saveFailed'));
    }
  }

  async function deleteRule(i: number) {
    const next = rules.filter((_, idx) => idx !== i);
    setStatus(`rule:${i}`, 'saving');
    if (await persistRules(next)) {
      rules = next;
      if (expanded === `rule:${i}`) doClose();
      toast('ok', t('modules.saved', { label: def.label }));
    } else {
      flagError(`rule:${i}`);
      toast('err', t('modules.saveFailed'));
    }
  }

  async function toggleRule(i: number) {
    const next = rules.map((r, idx) => (idx === i ? { ...r, enabled: !r.enabled } : r));
    setStatus(`rule:${i}`, 'saving');
    if (await persistRules(next)) {
      rules = next;
      ackSaved(`rule:${i}`);
    } else {
      flagError(`rule:${i}`);
      toast('err', t('modules.couldNotToggle', { label: rules[i].phrase }));
    }
  }

  // Whether the open reply/trigger editor has unsaved edits (drives the guard).
  const inspectorDirty = $derived.by(() => {
    if (!expanded) return false;
    if (isTriggers) {
      if (ruleIndex === null) return false;
      if (ruleIndex === -1) return draftPhrase.trim() !== '' || editMessage.trim() !== '';
      const r = rules[ruleIndex];
      return !r || draftPhrase !== r.phrase || draftMatch !== r.match || editMessage !== r.response;
    }
    if (selectedReply) return editMessage !== (config[selectedReply.messageKey] ?? '');
    return false;
  });

  // The inspector is open whenever a row (reply or rule) is expanded.
  const editing = $derived(!!expanded);
  const inspectorTitle = $derived(
    isTriggers ? (ruleIndex === -1 ? 'New trigger' : 'Edit trigger') : selectedReply ? selectedReply.label : t('modules.inspector')
  );

</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> {t('modules.allModules')}</a>
  <PageHead eyebrow={t('modules.detailEyebrow')} description={def.description}>{def.label}</PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  <!-- Settings strip: the module master switch (and any non-reply settings). -->
  <Card style="padding:0" class="settings-card">
    <div class="toggle-row">
      <div class="tr-text">
        <span class="tr-label">{t('modules.enabled')}</span>
        <span class="tr-help">{t('modules.enabledHelp')}</span>
      </div>
      <SaveStatus state={modStatus['module'] ?? 'idle'} />
      <Switch checked={enabled} label={t('modules.toggleAria', { label: def.label })} onchange={toggleModule} />
    </div>
    {#each def.settings ?? [] as field (field.key)}
      <div class="setting-row {field.type === 'textarea' ? 'stacked' : ''}">
        <label class="tr-text" for="mod-setting-{field.key}">
          <span class="tr-label">{field.label}</span>
          {#if field.help}<span class="tr-help">{field.help}</span>{/if}
        </label>
        <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
        {#if field.type === 'toggle'}
          <Switch
            checked={settingToggleOn(field)}
            label={`Toggle ${field.label}`}
            onchange={(v) => saveSetting(field, v ? 'on' : 'off')}
          />
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
        {:else if field.type === 'textarea'}
          <textarea
            id="mod-setting-{field.key}"
            class="setting-input setting-textarea"
            placeholder={field.placeholder ?? ''}
            value={config[field.key] ?? ''}
            onchange={(e) => saveSetting(field, e.currentTarget.value)}
          ></textarea>
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

  {#if !enabled && hasDeck}
    <!-- Off but configurable: state it plainly rather than dimming the surface. -->
    <AlertBanner>{t('modules.offConfigurable')}</AlertBanner>
  {/if}

  <!-- The deck: reply ledger + docked builder inspector (same as commands). A
       commands-only module (no editable replies) drops the inspector column and
       lists its chat commands read-only instead. Settings-only modules render no
       deck at all. -->
  {#if hasDeck}
  <div class="deck {editing ? 'inspecting' : ''} {hasInspector ? '' : 'commands-only'}">
    <DeckList>
      {#if isTriggers}
        <div class="rules-head">
          <div class="rh-text">
            <span class="rh-title">Trigger rules</span>
            <span class="rh-hint">Reply when a message matches a phrase — no "!" needed. First match wins.</span>
          </div>
          <button class="add-rule" type="button" onclick={addRule}><Icon name="plus" size={13} /> Add rule</button>
        </div>
        {#if ruleRows.length}
          <div class="list">
            {#each ruleRows as reply, i (reply.key)}
              <ReplyRow
                {reply}
                message={rules[i].response}
                index={i + 1}
                status={modStatus[reply.key] ?? 'idle'}
                expanded={expanded === reply.key}
                enabled={rules[i].enabled}
                onExpand={() => openRule(i)}
                onToggle={() => toggleRule(i)}
              />
            {/each}
          </div>
        {:else}
          <EmptyState icon="caps" title="No trigger words yet." body="Add a rule to reply automatically when a word shows up in chat." />
        {/if}
      {:else if hasReplies}
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
      {/if}

      {#if def.commands?.length}
        <div class="cmd-head">
          <span class="cmd-head-title">{t('modules.commandsTitle')}</span>
          <span class="cmd-head-hint">{t('modules.commandsHint')}</span>
        </div>
        <div class="list">
          {#each def.commands as command, i (command.trigger)}
            <ModuleCommandRow {command} index={i + 1} />
          {/each}
        </div>
      {/if}
    </DeckList>

    {#if hasInspector && editing}
      <InspectorSurface
        open
        title={inspectorTitle}
        controls="module-editor"
        closeLabel={t('modules.closeEditor')}
        onClose={closeInspector}
      >
        {#if isTriggers}
          <Scroller fill padding="16px" data-lenis-prevent>
            {#key expanded}
              <TriggerRuleEditor
                bind:phrase={draftPhrase}
                bind:match={draftMatch}
                bind:message={editMessage}
                {busy}
                isNew={ruleIndex === -1}
                onSave={saveRule}
                onCancel={closeInspector}
                onDelete={() => (ruleIndex !== null && ruleIndex >= 0 ? deleteRule(ruleIndex) : closeInspector())}
              />
            {/key}
          </Scroller>
        {:else if selectedReply}
          <Scroller fill padding="16px" data-lenis-prevent>
            {#key selectedReply.key}
              <ReplyEditor reply={selectedReply} bind:message={editMessage} {busy} onCancel={closeInspector} onSave={saveReply} />
            {/key}
          </Scroller>
        {/if}
      </InspectorSurface>
    {/if}
  </div>
  {/if}
</section>

<ConfirmDialog
  open={discardOpen}
  title={t('modules.discardTitle')}
  body={t('modules.discardBody')}
  confirmLabel={t('modules.discard')}
  cancelLabel={t('modules.keepEditing')}
  danger
  onCancel={cancelDiscard}
  onConfirm={confirmDiscard}
/>

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

  /* A textarea setting (e.g. trigger rules) stacks full-width under its label. */
  .setting-row.stacked { flex-direction: column; align-items: stretch; }
  .setting-row.stacked .tr-text { margin-right: 0; }
  .setting-textarea {
    width: 100%;
    min-height: 132px;
    padding: 10px 12px;
    line-height: 1.55;
    resize: vertical;
    white-space: pre;
    overflow-wrap: normal;
  }
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
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .list :global(.row-shell:last-child) { border-bottom: none; }

  /* Read-only command list header (commands-only modules, e.g. the queue). */
  .cmd-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 14px 8px;
    border-bottom: 1px solid var(--rule);
  }
  .cmd-head-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
  }
  .cmd-head-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  /* Trigger rules: a header with an add button above the add/removable rows. */
  .rules-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px 10px;
    border-bottom: 1px solid var(--rule);
  }
  .rh-text { display: flex; flex-direction: column; gap: 2px; margin-right: auto; min-width: 0; }
  .rh-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
  }
  .rh-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .add-rule {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-green-glow, #52b788);
    background: rgba(82, 183, 136, 0.06);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    border-radius: 999px;
    padding: 5px 12px;
    cursor: pointer;
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .add-rule:hover { background: rgba(82, 183, 136, 0.14); }
</style>
