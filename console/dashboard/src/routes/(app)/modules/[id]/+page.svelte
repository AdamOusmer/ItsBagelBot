<script lang="ts">
  import { deserialize } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { Card, PageHead, Scroller, SaveStatus, Switch, Button, InspectorSurface, ConfirmDialog, AlertBanner, DeckList, EmptyState, toast, getI18n, automodToggleDefault, type ModuleField, type ModuleReply } from '@bagel/shared';
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
  // Optimistic-concurrency token echoed on every patch; updated from each reply.
  // svelte-ignore state_referenced_locally
  let rev = $state<number>(data.revision ?? 0);

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
      rev = data.revision ?? 0;
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

  // Config writes are field-level patches (only the changed keys) under optimistic
  // concurrency. Writes are serialised so the revision stays consistent between
  // rapid edits; each write echoes the current rev and adopts the reply's. A
  // conflict means another writer moved the revision on: reload the latest state
  // and let the user redo, rather than silently clobbering their change.
  let writeChain: Promise<unknown> = Promise.resolve();

  async function runPatch(partial: Record<string, string>, en: boolean): Promise<boolean> {
    const body = new FormData();
    body.set('is_enabled', en ? 'on' : '');
    body.set('expected_rev', String(rev));
    body.set('partial', JSON.stringify(partial));
    const res = await fetch('?/patch', { method: 'POST', body }).catch(() => null);
    if (!res) return false;
    const result = deserialize(await res.text());
    const payload =
      result.type === 'success' || result.type === 'failure'
        ? (result.data as { ok?: boolean; rev?: number; conflict?: boolean } | undefined)
        : undefined;
    if (result.type === 'success' && payload?.ok) {
      if (typeof payload.rev === 'number') rev = payload.rev;
      return true;
    }
    if (payload?.conflict) {
      await invalidateAll();
      // The reseed effect only fires on module navigation, so refresh in place.
      enabled = data.enabled;
      config = { ...data.config };
      rev = data.revision ?? 0;
      rules = parseRules(data.config.rules ?? '');
      toast('err', t('modules.patchConflict'));
    }
    return false;
  }

  function patch(partial: Record<string, string>, en: boolean): Promise<boolean> {
    const result = writeChain.then(() => runPatch(partial, en));
    writeChain = result.catch(() => {});
    return result;
  }

  // --- Module master toggle (optimistic) -------------------------------------
  async function toggleModule() {
    const before = enabled;
    enabled = !enabled;
    setStatus('module', 'saving');
    if (await patch({}, enabled)) ackSaved('module');
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
    if (await patch({ [key]: value.trim() }, enabled)) ackSaved(`setting:${key}`);
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
    if (await patch({ [key]: was ? 'off' : 'on' }, enabled)) ackSaved(reply.key);
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
    const ok = await patch({ [r.messageKey]: editMessage }, enabled);
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

  const MODE_LABEL = $derived<Record<Match, string>>({
    word: t('modules.matchWord'),
    contains: t('modules.matchContains'),
    exact: t('modules.matchExact'),
    prefix: t('modules.matchPrefix')
  });

  function splitMode(left: string): [Match, string] {
    const c = left.indexOf(':');
    if (c < 0) return ['word', left];
    const pre = left.slice(0, c).trim().toLowerCase();
    if (pre === 'word' || pre === 'contains' || pre === 'exact' || pre === 'prefix') return [pre, left.slice(c + 1).trim()];
    return ['word', left];
  }
  // parseRules reads config.rules in either format: a value starting with "["
  // is the structured JSON array (written now); anything else is the legacy
  // "[mode:] phrase => response" line format (configs saved before the migration).
  function parseRules(raw: string): Rule[] {
    const s = raw.trim();
    if (!s) return [];
    if (s[0] === '[') return parseJSONRules(s);
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
  function parseJSONRules(s: string): Rule[] {
    const modes: Match[] = ['word', 'contains', 'exact', 'prefix'];
    try {
      const arr = JSON.parse(s);
      if (!Array.isArray(arr)) return [];
      return (arr as Array<Record<string, unknown>>)
        .map((r) => ({
          phrase: String(r.phrase ?? ''),
          response: String(r.response ?? ''),
          match: (modes.includes(r.match as Match) ? (r.match as Match) : 'word'),
          enabled: r.enabled !== false
        }))
        .filter((r) => r.phrase.trim() && r.response.trim());
    } catch {
      return [];
    }
  }
  // Structured JSON: sesame's parser reads this (and still reads the legacy line
  // format). JSON encodes any phrase safely, so "=>", a leading "#", or a "mode:"
  // prefix no longer corrupt the round trip.
  function serializeRules(list: Rule[]): string {
    return JSON.stringify(
      list
        .filter((r) => r.phrase.trim() && r.response.trim())
        .map((r) => ({
          phrase: r.phrase.trim(),
          response: r.response.replace(/\s*\n\s*/g, ' ').trim(),
          match: r.match,
          enabled: r.enabled
        }))
    );
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
    const rulesStr = serializeRules(next);
    const ok = await patch({ rules: rulesStr }, enabled);
    if (ok) config = { ...config, rules: rulesStr };
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

  async function saveRule() {
    if (ruleIndex === null) return;
    // Phrases are stored as structured JSON now, so any characters are safe —
    // no reserved-syntax restriction.
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
    isTriggers ? (ruleIndex === -1 ? t('modules.newTrigger') : t('modules.editTrigger')) : selectedReply ? selectedReply.label : t('modules.inspector')
  );

</script>

<section class="screen active">
  <!-- Breadcrumb: back ARROW to /modules; the current module marks aria-current. -->
  <nav class="crumbs" aria-label={t('modules.breadcrumbLabel')}>
    <ol>
      <li>
        <a class="crumb-back" href="/modules">
          <svg
            class="crumb-arrow"
            viewBox="0 0 24 24"
            width="14"
            height="14"
            aria-hidden="true"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <line x1="19" y1="12" x2="5" y2="12" />
            <polyline points="12 19 5 12 12 5" />
          </svg>
          {t('modules.allModules')}
        </a>
      </li>
      <li class="crumb-sep" aria-hidden="true">/</li>
      <li><span aria-current="page">{def.label}</span></li>
    </ol>
  </nav>

  <!-- PageHead: the module name is the page h1 (tabindex=-1 for route focus). -->
  <PageHead eyebrow={t('modules.detailEyebrow')} description={def.description}>{def.label}</PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  <!-- Module status + master switch. The state is spelled out (On/Off) beside the
       switch, never colour alone. Toggling keeps main's optimistic field-level
       patch, so this stays the page's own Switch. -->
  <Card style="padding:0" class="settings-card">
    <div class="master-row">
      <div class="tr-text">
        <h2 class="tr-label">{t('modules.moduleStatus')}</h2>
        <span class="tr-help">{t('modules.enabledHelp')}</span>
      </div>
      <span class="status-text" class:on={enabled}>{enabled ? t('modules.statusOn') : t('modules.statusOff')}</span>
      <SaveStatus state={modStatus['module'] ?? 'idle'} />
      <Switch
        checked={enabled}
        label={t('modules.toggleAria', { label: def.label })}
        pending={modStatus['module'] === 'saving'}
        onchange={toggleModule}
      />
    </div>
    {#if !enabled}
      <!-- Off but configurable: explain in TEXT that the lists stay editable and
           go live once turned on, rather than dimming the whole surface. -->
      <p class="disabled-note">{t('modules.disabledNote')}</p>
    {/if}
  </Card>

  <!-- General settings. Each field auto-saves on change through main's field-level
       patch (only the changed key posts under optimistic concurrency). -->
  {#if (def.settings ?? []).length}
    <Card style="padding:0" class="settings-card">
      <div class="section-head">
        <h2 class="section-title">{t('modules.settingsTitle')}</h2>
      </div>
      {#each def.settings ?? [] as field (field.key)}
        {#if field.type === 'toggle'}
          <div class="setting-row">
            <div class="tr-text">
              <span class="tr-label" id="sl-{field.key}">{field.label}</span>
              {#if field.help}<span class="tr-help" id="sh-{field.key}">{field.help}</span>{/if}
            </div>
            <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
            <Switch
              checked={settingToggleOn(field)}
              label={field.label}
              describedby={field.help ? `sh-${field.key}` : undefined}
              pending={modStatus[`setting:${field.key}`] === 'saving'}
              onchange={(v) => saveSetting(field, v ? 'on' : 'off')}
            />
          </div>
        {:else}
          <div class="setting-row {field.type === 'textarea' ? 'stacked' : ''}">
            <label class="tr-text" for="mod-setting-{field.key}">
              <span class="tr-label">{field.label}</span>
              {#if field.help}<span class="tr-help">{field.help}</span>{/if}
            </label>
            <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
            {#if field.type === 'select'}
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
        {/if}
      {/each}
    </Card>
  {/if}

  <!-- The deck: reply/trigger ledger + docked builder inspector (same as commands).
       A commands-only module (no editable replies) drops the inspector column and
       lists its chat commands read-only instead. Settings-only modules render no
       deck at all. -->
  {#if hasDeck}
    <div class="deck" class:inspecting={editing && hasInspector}>
      <DeckList>
        {#if isTriggers}
          <div class="section-head rules-head">
            <div class="rh-text">
              <h2 class="section-title">{t('modules.triggerRulesTitle')}</h2>
              <span class="rh-hint">{t('modules.triggerRulesHint')}</span>
            </div>
            <Button variant="ghost" icon="plus" onclick={addRule}>{t('modules.addTrigger')}</Button>
          </div>
          {#if ruleRows.length}
            <ul class="list" aria-label={t('modules.triggerRulesTitle')}>
              {#each ruleRows as reply, i (reply.key)}
                <li>
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
                </li>
              {/each}
            </ul>
          {:else}
            <EmptyState icon="caps" title={t('modules.noTriggersTitle')} body={t('modules.noTriggersBody')} />
          {/if}
        {:else if hasReplies}
          <ul class="list" aria-label={t('modules.repliesLabel')}>
            {#each def.replies as reply, i (reply.key)}
              <li>
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
              </li>
            {/each}
          </ul>
        {/if}

        {#if def.commands?.length}
          <div class="section-head cmd-head">
            <h2 class="section-title">{t('modules.commandsTitle')}</h2>
            <span class="cmd-head-hint">{t('modules.commandsHint')}</span>
          </div>
          <ul class="list" aria-label={t('modules.commandsTitle')}>
            {#each def.commands as command, i (command.trigger)}
              <li><ModuleCommandRow {command} index={i + 1} /></li>
            {/each}
          </ul>
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
  /* ── breadcrumb ── */
  .crumbs { margin-bottom: 10px; }
  .crumbs ol {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .crumb-back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 44px;
    margin: -8px 0;
    color: var(--bb-muted);
    text-decoration: none;
  }
  .crumb-back:hover { color: var(--bb-white); }
  .crumb-back:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: 2px; border-radius: 4px; }
  .crumb-arrow { flex: none; }
  .crumb-sep { opacity: 0.5; }
  .crumbs [aria-current='page'] { color: var(--bb-tan-light); }

  /* ── settings cards ── */
  :global(.settings-card) { margin-bottom: 16px; }
  .master-row { display: flex; align-items: center; gap: 12px; padding: 16px 18px; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }
  .tr-label { margin: 0; font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  /* Status word — never colour alone: the text says On/Off, colour only tints. */
  .status-text {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .status-text.on { color: var(--bb-green-glow, #52b788); }

  /* A disabled module stays fully readable: no page-wide dimming, just a note
     spelling out that the lists below are inactive until it is turned on. */
  .disabled-note {
    margin: 0;
    padding: 12px 18px 16px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    border-top: 1px solid var(--rule);
  }

  /* Section header (Settings, Trigger rules, Commands): a small tan caption. */
  .section-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--rule);
  }
  .section-title {
    margin: 0;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
  }

  /* Plain settings fields under the section head (e.g. linked account). */
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

  /* A textarea setting (e.g. blocked terms) stacks full-width under its label. */
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

  /* Semantic list of rows; the last row drops its divider inside the card. */
  .list { list-style: none; margin: 0; padding: 0; }
  .list > li:last-child :global(.row-shell) { border-bottom: none; }

  /* Trigger-rules header: title + hint on the left, add button pushed right. */
  .rh-text { display: flex; flex-direction: column; gap: 2px; margin-right: auto; min-width: 0; }
  .rh-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  /* Read-only command list header stacks its title and hint. */
  .cmd-head { flex-direction: column; align-items: flex-start; gap: 2px; }
  .cmd-head-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
</style>
