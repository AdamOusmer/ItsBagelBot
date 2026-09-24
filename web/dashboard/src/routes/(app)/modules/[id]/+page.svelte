<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { Card, PageHead, Scroller, SaveStatus, Switch, Button, ButtonLink, InspectorSurface, ConfirmDialog, AlertBanner, DeckList, EmptyState, toast, getI18n, automodToggleDefault, moduleDef, tModuleLabel, tModuleDescription, tModuleFieldPart, tModuleFieldOption, tModuleReplyPart, type ModuleField, type ModuleReply, MOD } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ReplyRow from '$lib/components/modules/ReplyRow.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import ReplyEditor from '$lib/components/modules/ReplyEditor.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import TriggerRuleEditor from '$lib/components/modules/TriggerRuleEditor.svelte';
  import TimezonePicker from '$lib/components/modules/TimezonePicker.svelte';

  let { data } = $props();

  const { t } = getI18n();
  const def = $derived(data.def);
  const modLabel = $derived(tModuleLabel(t, def));
  const modDescription = $derived(tModuleDescription(t, def));
  const hasReplies = $derived(def.replies.length > 0);
  const parentDef = $derived(def.parent ? moduleDef(def.parent) : undefined);

  // svelte-ignore state_referenced_locally
  let enabled = $state(data.enabled);
  // svelte-ignore state_referenced_locally
  let config = $state<Record<string, string>>({ ...data.config });
  // svelte-ignore state_referenced_locally
  let rev = $state<number>(data.revision ?? 0);

  const isTriggers = $derived(def.id === MOD.triggers);
  // svelte-ignore state_referenced_locally
  let rules = $state<Rule[]>(parseRules(data.config.rules ?? ''));
  const hasInspector = $derived(isTriggers || hasReplies);
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

  let modStatus = $state<Record<string, SaveState>>({});
  const timers = new Map<string, ReturnType<typeof setTimeout>[]>();
  function setStatus(key: string, s: SaveState) {
    for (const tm of timers.get(key) ?? []) clearTimeout(tm);
    timers.delete(key);
    modStatus = { ...modStatus, [key]: s };
  }
  function ackSaved(key: string) {
    setStatus(key, 'saved');
    timers.set(key, [setTimeout(() => (modStatus = { ...modStatus, [key]: 'idle' }), 3000)]);
  }
  function flagError(key: string) {
    setStatus(key, 'error');
    timers.set(key, [setTimeout(() => (modStatus = { ...modStatus, [key]: 'idle' }), 4000)]);
  }

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

  async function toggleModule() {
    const before = enabled;
    enabled = !enabled;
    setStatus('module', 'saving');
    if (await patch({}, enabled)) ackSaved('module');
    else {
      enabled = before;
      flagError('module');
      toast('err', t('modules.couldNotToggle', { label: modLabel }));
    }
  }

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

  function settingToggleOn(field: ModuleField): boolean {
    const v = config[field.key] ?? '';
    if (v === 'on') return true;
    if (v === 'off') return false;
    return field.followsLevel ? automodToggleDefault(config['level'] || 'moderate', field.key) : false;
  }

  function replyOn(reply: ModuleReply): boolean {
    const v = config[reply.enableKey ?? ''] ?? '';
    if (v === 'on') return true;
    if (v === 'off') return false;
    return !reply.defaultOff;
  }

  async function toggleReply(reply: ModuleReply) {
    if (!reply.enableKey) return;
    const key = reply.enableKey;
    const was = replyOn(reply);
    config = { ...config, [key]: was ? 'off' : 'on' };
    setStatus(reply.key, 'saving');
    if (await patch({ [key]: was ? 'off' : 'on' }, enabled)) ackSaved(reply.key);
    else {
      config = { ...config, [key]: was ? 'on' : 'off' };
      flagError(reply.key);
      toast('err', t('modules.couldNotToggle', { label: tModuleReplyPart(t, def.id, reply, 'label') }));
    }
  }

  let expanded = $state<string | null>(null);
  let editMessage = $state('');
  let busy = $state(false);
  const selectedReply = $derived(expanded ? def.replies.find((r) => r.key === expanded) : undefined);

  const discard = createDiscardGuard(() => inspectorDirty);
  function doClose() {
    expanded = null;
    ruleIndex = null;
  }

  function openReply(reply: ModuleReply) {
    if (expanded === reply.key) {
      closeInspector();
      return;
    }
    discard.guard(() => {
      editMessage = config[reply.messageKey] ?? '';
      expanded = reply.key;
    });
  }
  function closeInspector() {
    discard.guard(doClose);
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
      toast('ok', t('modules.saved', { label: modLabel }));
    } else {
      config = { ...config, [r.messageKey]: prev ?? '' };
      flagError(r.key);
      toast('err', t('modules.saveFailed'));
    }
  }

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
        if (!rest.includes('=>')) continue;
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

  let ruleIndex = $state<number | null>(null);
  let draftPhrase = $state('');
  let draftMatch = $state<Match>('word');

  async function persistRules(next: Rule[]): Promise<boolean> {
    const rulesStr = serializeRules(next);
    const ok = await patch({ rules: rulesStr }, enabled);
    if (ok) config = { ...config, rules: rulesStr };
    return ok;
  }

  function openRule(i: number) {
    if (expanded === `rule:${i}`) return closeInspector();
    discard.guard(() => {
      const r = rules[i];
      ruleIndex = i;
      draftPhrase = r.phrase;
      draftMatch = r.match;
      editMessage = r.response;
      expanded = `rule:${i}`;
    });
  }
  function addRule() {
    discard.guard(() => {
      ruleIndex = -1;
      draftPhrase = '';
      draftMatch = 'word';
      editMessage = '';
      expanded = 'rule:new';
    });
  }

  async function saveRule() {
    if (ruleIndex === null) return;
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
      if (ruleIndex === -1) {
        ruleIndex = next.length - 1;
        expanded = `rule:${next.length - 1}`;
      }
      toast('ok', t('modules.saved', { label: modLabel }));
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
      toast('ok', t('modules.saved', { label: modLabel }));
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

  let browserZone = $state('');
  let tzZones = $state<string[]>([]);
  $effect(() => {
    try {
      browserZone = new Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
      const list: string[] = Intl.supportedValuesOf?.('timeZone') ?? [];
      tzZones = browserZone && !list.includes(browserZone) ? [...list, browserZone].sort() : list;
    } catch {
      tzZones = browserZone ? [browserZone] : [];
    }
  });

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

  const editing = $derived(!!expanded);
  const inspectorTitle = $derived(
    isTriggers ? (ruleIndex === -1 ? t('modules.newTrigger') : t('modules.editTrigger')) : selectedReply ? tModuleReplyPart(t, def.id, selectedReply, 'label') : t('modules.inspector')
  );

  function fieldCopy(field: ModuleField, part: 'label' | 'help' | 'placeholder'): string {
    return tModuleFieldPart(t, def.id, field, part);
  }

</script>

<section class="screen active">
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
      <li><span aria-current="page">{modLabel}</span></li>
    </ol>
  </nav>

  <PageHead eyebrow={t('modules.detailEyebrow')} description={modDescription}>{modLabel}</PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  {#if data.locked}
    <AlertBanner variant="warn">
      {t('modules.betaLockedBody')}
      {#snippet action()}
        <ButtonLink href="/billing" variant="ghost">{t('modules.betaUpgrade')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  {#if parentDef}
    <AlertBanner variant="warn">
      {t('modules.nestedUnder', { parent: tModuleLabel(t, parentDef) })}
      {#snippet action()}
        <ButtonLink href={parentDef.href ?? `/modules/${parentDef.id}`} variant="ghost">{t('modules.nestedUnderLink', { parent: tModuleLabel(t, parentDef) })}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  {#if !def.parent}
  <Card style="padding:0" class="settings-card">
    <div class="master-row">
      <div class="tr-text">
        <h2 class="tr-label">{t('modules.moduleStatus')}</h2>
        <span class="tr-help">{t('modules.enabledHelp')}</span>
      </div>
      {#if data.locked}
        <span class="status-text bb-tag bb-tag--quiet"><i class="bb-mark bb-mark--hollow" aria-hidden="true"></i>{t('modules.betaLocked')}</span>
      {:else}
        <span class="status-text bb-tag {enabled ? 'bb-tag--live' : 'bb-tag--quiet'}">
          <i class="bb-mark {enabled ? '' : 'bb-mark--hollow'}" aria-hidden="true"></i>
          {enabled ? t('modules.statusOn') : t('modules.statusOff')}
        </span>
        <SaveStatus state={modStatus['module'] ?? 'idle'} />
        <Switch
          checked={enabled}
          label={t('modules.toggleAria', { label: modLabel })}
          pending={modStatus['module'] === 'saving'}
          onchange={toggleModule}
        />
      {/if}
    </div>
    {#if !enabled}
      <p class="disabled-note">{t('modules.disabledNote')}</p>
    {/if}
  </Card>
  {/if}

  {#if (def.settings ?? []).length}
    <Card style="padding:0" class="settings-card">
      <div class="section-head">
        <h2 class="section-title">{t('modules.settingsTitle')}</h2>
      </div>
      {#each (def.settings ?? []).filter((s) => !s.hidden) as field (field.key)}
        {#if field.type === 'toggle'}
          <div class="setting-row">
            <div class="tr-text">
              <span class="tr-label" id="sl-{field.key}">{fieldCopy(field, 'label')}</span>
              {#if fieldCopy(field, 'help')}<span class="tr-help" id="sh-{field.key}">{fieldCopy(field, 'help')}</span>{/if}
            </div>
            <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
            <Switch
              checked={settingToggleOn(field)}
              label={fieldCopy(field, 'label')}
              describedby={fieldCopy(field, 'help') ? `sh-${field.key}` : undefined}
              pending={modStatus[`setting:${field.key}`] === 'saving'}
              onchange={(v) => saveSetting(field, v ? 'on' : 'off')}
            />
          </div>
        {:else if field.type === 'timezone'}
          <div class="setting-row">
            <label class="tr-text" for="mod-setting-{field.key}">
              <span class="tr-label">{fieldCopy(field, 'label')}</span>
              {#if fieldCopy(field, 'help')}<span class="tr-help">{fieldCopy(field, 'help')}</span>{/if}
            </label>
            <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
            <TimezonePicker id="mod-setting-{field.key}" value={config[field.key] ?? ''} zones={tzZones} onPick={(tz) => saveSetting(field, tz)} />
          </div>
          {#if browserZone && (config[field.key] ?? '') !== browserZone}
            <div class="tz-suggest">
              <span class="tz-suggest-text">{t('modules.tzSuggested', { tz: browserZone })}</span>
              <Button variant="green" size="sm" onclick={() => saveSetting(field, browserZone)}>{t('modules.tzApply', { tz: browserZone })}</Button>
            </div>
          {/if}
        {:else}
          <div class="setting-row {field.type === 'textarea' ? 'stacked' : ''}">
            <label class="tr-text" for="mod-setting-{field.key}">
              <span class="tr-label">{fieldCopy(field, 'label')}</span>
              {#if fieldCopy(field, 'help')}<span class="tr-help">{fieldCopy(field, 'help')}</span>{/if}
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
                  <option value={opt.value}>{tModuleFieldOption(t, def.id, field.key, opt)}</option>
                {/each}
              </select>
            {:else if field.type === 'textarea'}
              <textarea
                id="mod-setting-{field.key}"
                class="setting-input setting-textarea"
                placeholder={fieldCopy(field, 'placeholder')}
                value={config[field.key] ?? ''}
                onchange={(e) => saveSetting(field, e.currentTarget.value)}
              ></textarea>
            {:else}
              <input
                id="mod-setting-{field.key}"
                class="setting-input"
                type={field.type === 'number' ? 'number' : 'text'}
                placeholder={fieldCopy(field, 'placeholder')}
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

  {#if hasDeck}
    <div class="deck" class:inspecting={editing && hasInspector}>
      <DeckList>
        {#if isTriggers}
          <div class="section-head rules-head">
            <div class="rh-text">
              <h2 class="section-title">{t('modules.triggerRulesTitle')}</h2>
              <span class="rh-hint">{t('modules.triggerRulesHint')}</span>
            </div>
            <Button variant="ghost" onclick={addRule}>{t('modules.addTrigger')}</Button>
          </div>
          {#if ruleRows.length}
            <ul class="list" aria-label={t('modules.triggerRulesTitle')}>
              {#each ruleRows as reply, i (reply.key)}
                <li>
                  <ReplyRow
                    moduleId={def.id}
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
            <EmptyState title={t('modules.noTriggersTitle')} body={t('modules.noTriggersBody')} />
          {/if}
        {:else if hasReplies}
          <ul class="list" aria-label={t('modules.repliesLabel')}>
            {#each def.replies as reply, i (reply.key)}
              <li>
                <ReplyRow
                  moduleId={def.id}
                  {reply}
                  message={config[reply.messageKey] ?? ''}
                  index={i + 1}
                  status={modStatus[reply.key] ?? 'idle'}
                  expanded={expanded === reply.key}
                  enabled={reply.enableKey ? replyOn(reply) : undefined}
                  onExpand={() => openReply(reply)}
                  onToggle={() => toggleReply(reply)}
                />
              </li>
            {/each}
          </ul>
        {/if}

        <ModuleCommandList moduleId={def.id} commands={def.commands ?? []} />
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
            <Scroller fill padding="16px" smooth>
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
            <Scroller fill padding="16px" smooth>
              {#key selectedReply.key}
                <ReplyEditor moduleId={def.id} reply={selectedReply} bind:message={editMessage} {busy} onCancel={closeInspector} onSave={saveReply} />
              {/key}
            </Scroller>
          {/if}
        </InspectorSurface>
      {/if}
    </div>
  {/if}
</section>

<ConfirmDialog
  open={discard.open}
  title={t('modules.discardTitle')}
  body={t('modules.discardBody')}
  confirmLabel={t('modules.discard')}
  cancelLabel={t('modules.keepEditing')}
  danger
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
/>

<style>
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
  .crumb-back:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: 2px; border-radius: var(--bb-radius-xs); }
  .crumb-arrow { flex: none; }
  .crumb-sep { opacity: 0.5; }
  .crumbs [aria-current='page'] { color: var(--bb-tan-light); }

  :global(.settings-card) { margin-bottom: 16px; }
  .master-row { display: flex; align-items: center; gap: 12px; padding: 16px 18px; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }
  .tr-label { margin: 0; font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .status-text { flex: none; }

  .disabled-note {
    margin: 0;
    padding: 12px 18px 16px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    border-top: 1px solid var(--rule);
  }

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
    border-radius: var(--bb-radius-sm);
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

  .tz-suggest {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 0 18px 14px;
    padding: 10px 12px 10px 14px;
    border: 1px solid rgba(82, 183, 136, 0.35);
    border-radius: var(--bb-radius-md);
    background: rgba(82, 183, 136, 0.07);
  }
  .tz-suggest-text {
    margin-right: auto;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-white);
  }
  @media (max-width: 560px) {
    .tz-suggest { flex-wrap: wrap; }
  }

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting { grid-template-columns: minmax(0, 1fr) 420px; }
  }

  .list { list-style: none; margin: 0; padding: 0; }
  .list > li:last-child :global(.row-shell) { border-bottom: none; }

  .rh-text { display: flex; flex-direction: column; gap: 2px; margin-right: auto; min-width: 0; }
  .rh-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

</style>
