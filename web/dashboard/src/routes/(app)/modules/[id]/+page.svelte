<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Select } from '@bagel/ui/svelte';
  import { namespaceReplyTemplate } from '@bagel/kit';
  import { deserialize } from '$app/forms';
  import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
  import { Card, PageHead, Scroller, SectionNav, SearchInput, SaveStatus, Switch, SwitchRow, Button, ButtonLink, InspectorSurface, ConfirmDialog, AlertBanner, DeckLayout, DeckList, EmptyState, Heading, Input, Tag, Text, Textarea, TextLink, toast } from '@bagel/ui/svelte';
  import { getI18n, automodToggleDefault, moduleDef, tModuleLabel, tModuleDescription, tModuleFieldPart, tModuleFieldOption, tModuleReplyPart, type ModuleField, type ModuleReply, MOD } from '@bagel/kit';
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
  const locked = $derived(data.locked === true);
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

  type PatchOutcome = 'saved' | 'conflict' | 'failed';
  let writeChain: Promise<unknown> = Promise.resolve();
  let writeGeneration = 0;
  type RefetchEdits = { partial: Record<string, string>; enabled?: boolean };
  let refetchEdits: RefetchEdits | null = null;

  async function reloadAfterConflict() {
    // Cancel queued edits drafted against the state being replaced.
    writeGeneration += 1;
    // Edits made during the refetch are queued against the new revision, so keep them.
    const edits: RefetchEdits = { partial: {} };
    refetchEdits = edits;
    await invalidateAll();
    refetchEdits = null;
    // The reseed effect only fires on module navigation, so refresh in place.
    enabled = edits.enabled ?? data.enabled;
    config = { ...data.config, ...edits.partial };
    rev = data.revision ?? 0;
    rules = parseRules(config.rules ?? '');
  }

  async function runPatch(partial: Record<string, string>, en: boolean): Promise<PatchOutcome> {
    const body = new FormData();
    body.set('is_enabled', en ? 'on' : '');
    body.set('expected_rev', String(rev));
    body.set('partial', JSON.stringify(partial));
    const res = await fetch('?/patch', { method: 'POST', body }).catch(() => null);
    if (!res) return 'failed';
    const result = deserialize(await res.text());
    const payload =
      result.type === 'success' || result.type === 'failure'
        ? (result.data as { ok?: boolean; rev?: number; conflict?: boolean } | undefined)
        : undefined;
    if (result.type === 'success' && payload?.ok) {
      if (typeof payload.rev === 'number') rev = payload.rev;
      return 'saved';
    }
    if (payload?.conflict) {
      await reloadAfterConflict();
      toast('danger', t('modules.patchConflict'));
      return 'conflict';
    }
    return 'failed';
  }

  function patch(partial: Record<string, string>, en: boolean): Promise<PatchOutcome> {
    if (refetchEdits) {
      Object.assign(refetchEdits.partial, partial);
      refetchEdits.enabled = en;
    }
    const generation = writeGeneration;
    const result = writeChain.then(() => generation === writeGeneration ? runPatch(partial, en) : 'conflict' as const);
    writeChain = result.catch(() => {});
    return result;
  }

  async function toggleModule() {
    const before = enabled;
    enabled = !enabled;
    setStatus('module', 'saving');
    const outcome = await patch({}, enabled);
    if (outcome === 'saved') ackSaved('module');
    else {
      flagError('module');
      if (outcome === 'failed') {
        enabled = before;
        toast('danger', t('modules.couldNotToggle', { label: modLabel }));
      }
    }
  }

  async function saveSetting(field: ModuleField, value: string) {
    const key = field.key;
    const before = config[key] ?? '';
    if (value.trim() === before.trim()) return;
    config = { ...config, [key]: value.trim() };
    setStatus(`setting:${key}`, 'saving');
    const outcome = await patch({ [key]: value.trim() }, enabled);
    if (outcome === 'saved') ackSaved(`setting:${key}`);
    else {
      flagError(`setting:${key}`);
      if (outcome === 'failed') {
        config = { ...config, [key]: before };
        toast('danger', t('modules.saveFailed'));
      }
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
    const outcome = await patch({ [key]: was ? 'off' : 'on' }, enabled);
    if (outcome === 'saved') ackSaved(reply.key);
    else {
      flagError(reply.key);
      if (outcome === 'failed') {
        config = { ...config, [key]: was ? 'on' : 'off' };
        toast('danger', t('modules.couldNotToggle', { label: tModuleReplyPart(t, def.id, reply, 'label') }));
      }
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
    if (locked) return;
    if (expanded === reply.key) {
      closeInspector();
      return;
    }
    discard.guard(() => {
      editMessage = namespaceReplyTemplate(def.id, reply, config[reply.messageKey] ?? '');
      expanded = reply.key;
    });
  }
  function closeInspector() {
    discard.guard(doClose);
  }
  beforeNavigate((nav) => {
    if (!inspectorDirty || !nav.to) return;
    nav.cancel();
    const href = nav.to.url.href;
    discard.guard(() => {
      doClose();
      void goto(href);
    });
  });

  async function saveReply() {
    const r = selectedReply;
    if (!r) return;
    editMessage = namespaceReplyTemplate(def.id, r, editMessage);
    const prev = config[r.messageKey];
    config = { ...config, [r.messageKey]: editMessage };
    busy = true;
    setStatus(r.key, 'saving');
    const outcome = await patch({ [r.messageKey]: editMessage }, enabled);
    busy = false;
    if (outcome === 'saved') {
      ackSaved(r.key);
      // Save keeps the inspector open on the saved reply (now clean); no close.
      toast('success', t('modules.saved', { label: modLabel }));
    } else {
      flagError(r.key);
      if (outcome === 'failed') {
        config = { ...config, [r.messageKey]: prev ?? '' };
        toast('danger', t('modules.saveFailed'));
      }
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
      label: r.phrase || t('modules.newRule'),
      tagline: MODE_LABEL[r.match],
      event: t('modules.ruleEvent', { phrase: r.phrase || '…' }),
      messageKey: `rule:${i}`,
      enableKey: `rule:${i}`,
      defaultMessage: r.response
    }))
  );

  let ruleIndex = $state<number | null>(null);
  let draftPhrase = $state('');
  let draftMatch = $state<Match>('word');

  async function persistRules(next: Rule[]): Promise<PatchOutcome> {
    const rulesStr = serializeRules(next);
    const outcome = await patch({ rules: rulesStr }, enabled);
    if (outcome === 'saved') config = { ...config, rules: rulesStr };
    return outcome;
  }

  function openRule(i: number) {
    if (locked) return;
    if (expanded === `rule:${i}`) return closeInspector();
    discard.guard(() => {
      const r = rules[i];
      ruleIndex = i;
      draftPhrase = r.phrase;
      draftMatch = r.match;
      editMessage = namespaceReplyTemplate('triggers', { tokens: [{ name: 'triggers:user', sample: '' }, { name: 'triggers:channel', sample: '' }] }, r.response);
      expanded = `rule:${i}`;
    });
  }
  function addRule() {
    if (locked) return;
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
    editMessage = namespaceReplyTemplate('triggers', { tokens: [{ name: 'triggers:user', sample: '' }, { name: 'triggers:channel', sample: '' }] }, editMessage);
    // Phrases are stored as structured JSON now, so any characters are safe:
    // no reserved-syntax restriction.
    const keepOn = ruleIndex === -1 ? true : (rules[ruleIndex]?.enabled ?? true);
    const draft: Rule = { phrase: draftPhrase.trim(), response: editMessage, match: draftMatch, enabled: keepOn };
    const next = ruleIndex === -1 ? [...rules, draft] : rules.map((r, i) => (i === ruleIndex ? draft : r));
    const key = expanded ?? 'rule';
    busy = true;
    setStatus(key, 'saving');
    const outcome = await persistRules(next);
    busy = false;
    if (outcome === 'saved') {
      rules = next;
      // Keep the inspector open on the saved rule (a new rule becomes the last
      // row); it now reads clean.
      if (ruleIndex === -1) {
        ruleIndex = next.length - 1;
        expanded = `rule:${next.length - 1}`;
      }
      toast('success', t('modules.saved', { label: modLabel }));
    } else {
      flagError(key);
      if (outcome === 'failed') toast('danger', t('modules.saveFailed'));
    }
  }

  let deleteIndex = $state<number | null>(null);
  const deletePhrase = $derived(deleteIndex === null ? '' : (rules[deleteIndex]?.phrase ?? ''));
  function askDeleteRule() {
    if (ruleIndex !== null && ruleIndex >= 0) deleteIndex = ruleIndex;
    else closeInspector();
  }
  function confirmDeleteRule() {
    const i = deleteIndex;
    deleteIndex = null;
    if (i !== null) void deleteRule(i);
  }

  async function deleteRule(i: number) {
    const next = rules.filter((_, idx) => idx !== i);
    busy = true;
    setStatus(`rule:${i}`, 'saving');
    const outcome = await persistRules(next);
    busy = false;
    if (outcome === 'saved') {
      rules = next;
      if (expanded === `rule:${i}`) doClose();
      toast('success', t('modules.saved', { label: modLabel }));
    } else {
      flagError(`rule:${i}`);
      if (outcome === 'failed') toast('danger', t('modules.saveFailed'));
    }
  }

  async function toggleRule(i: number) {
    const next = rules.map((r, idx) => (idx === i ? { ...r, enabled: !r.enabled } : r));
    setStatus(`rule:${i}`, 'saving');
    const outcome = await persistRules(next);
    if (outcome === 'saved') {
      rules = next;
      ackSaved(`rule:${i}`);
    } else {
      flagError(`rule:${i}`);
      if (outcome === 'failed') toast('danger', t('modules.couldNotToggle', { label: rules[i].phrase }));
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
      return !r || draftPhrase !== r.phrase || draftMatch !== r.match || editMessage !== namespaceReplyTemplate('triggers', { tokens: [{ name: 'triggers:user', sample: '' }, { name: 'triggers:channel', sample: '' }] }, r.response);
    }
    if (selectedReply) return editMessage !== namespaceReplyTemplate(def.id, selectedReply, config[selectedReply.messageKey] ?? '');
    return false;
  });

  const editing = $derived(!!expanded);
  const inspectorTitle = $derived(
    isTriggers ? (ruleIndex === -1 ? t('modules.newTrigger') : t('modules.editTrigger')) : selectedReply ? tModuleReplyPart(t, def.id, selectedReply, 'label') : t('modules.inspector')
  );

  let replyQuery = $state('');
  const showReplyFilter = $derived(!isTriggers && def.replies.length > 6);
  const visibleReplies = $derived.by(() => {
    const q = replyQuery.trim().toLowerCase();
    const all = def.replies.map((reply, i) => ({ reply, i }));
    if (!q || !showReplyFilter) return all;
    return all.filter(({ reply }) =>
      `${tModuleReplyPart(t, def.id, reply, 'label')}\n${config[reply.messageKey] ?? ''}`.toLowerCase().includes(q)
    );
  });

  const hasSettings = $derived((def.settings ?? []).some((s) => !s.hidden));
  const hasCommands = $derived((def.commands?.length ?? 0) > 0);
  const navItems = $derived([
    ...(hasSettings ? [{ href: '#mod-settings', label: t('modules.settingsTitle') }] : []),
    ...(isTriggers || hasReplies ? [{ href: '#mod-replies', label: isTriggers ? t('modules.triggerRulesTitle') : t('modules.repliesLabel') }] : []),
    ...(hasCommands ? [{ href: '#mod-commands', label: t('modules.commandsTitle') }] : [])
  ]);

  function fieldCopy(field: ModuleField, part: 'label' | 'help' | 'placeholder'): string {
    return tModuleFieldPart(t, def.id, field, part);
  }

  function blurOnEnter(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (e.key === 'Enter') e.currentTarget.blur();
  }

</script>

<section class="screen active">
  <nav class="crumbs" aria-label={t('modules.breadcrumbLabel')}>
    <ol>
      <li><TextLink variant="quiet" touch icon="arrowLeft" href="/modules" label={t('modules.allModules')} /></li>
      <li class="crumb-sep" aria-hidden="true"><Text as="span" size="sm" tone="muted">/</Text></li>
      <li><Text as="span" size="sm" tone="accent" aria-current="page">{modLabel}</Text></li>
    </ol>
  </nav>

  <PageHead eyebrow={t('modules.detailEyebrow')} description={modDescription}>{modLabel}</PageHead>

  {#if data.degraded}
    <AlertBanner>{t('modules.degraded')}</AlertBanner>
  {/if}

  {#if data.locked}
    <AlertBanner tone="warning">
      {t('modules.betaLockedBody')}
      {#snippet actions()}
        <ButtonLink href="/billing" variant="ghost">{t('modules.betaUpgrade')}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  {#if parentDef}
    <AlertBanner tone="warning">
      {t('modules.nestedUnder', { parent: tModuleLabel(t, parentDef) })}
      {#snippet actions()}
        <ButtonLink href={parentDef.href ?? `/modules/${parentDef.id}`} variant="ghost">{t('modules.nestedUnderLink', { parent: tModuleLabel(t, parentDef) })}</ButtonLink>
      {/snippet}
    </AlertBanner>
  {/if}

  {#if navItems.length > 1}
    <SectionNav label={t('modules.sectionNav')} items={navItems} />
  {/if}

  {#if !def.parent}
  <div class="settings-card">
    <Card flush>
      <div class="master-row">
        <div class="tr-text">
          <Heading level={6} as="h2">{t('modules.moduleStatus')}</Heading>
          <Text as="span" size="xs" tone="muted">{t('modules.enabledHelp')}</Text>
        </div>
        {#if data.locked}
          <Tag tone="quiet" mark="hollow">{t('modules.betaLocked')}</Tag>
        {:else}
          <Tag tone={enabled ? 'live' : 'quiet'} mark={enabled ? 'solid' : 'hollow'}>
            {enabled ? t('modules.statusOn') : t('modules.statusOff')}
          </Tag>
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
        <div class="disabled-note"><Text size="sm" tone="muted">{t('modules.disabledNote')}</Text></div>
      {/if}
    </Card>
  </div>
  {/if}

  {#if (def.settings ?? []).length}
    <div id="mod-settings" class="settings-card anchor" tabindex="-1">
      <Card flush>
        <div class="section-head">
          <Heading level={6} as="h2" variant="eyebrow">{t('modules.settingsTitle')}</Heading>
          {#if locked}
            <span class="lock-hint"><Tag tone="quiet">{t('modules.betaPremium')}</Tag></span>
          {:else}
            <span class="autosave"><Text as="span" size="xs" tone="muted">{t('modules.autoSaveNote')}</Text></span>
          {/if}
        </div>
        {#each (def.settings ?? []).filter((s) => !s.hidden) as field (field.key)}
          {#if field.type === 'toggle'}
            <div class="setting-row toggle">
              <SwitchRow
                control="end"
                label={fieldCopy(field, 'label')}
                hint={fieldCopy(field, 'help') || undefined}
                hintId="sh-{field.key}"
                checked={settingToggleOn(field)}
                pending={modStatus[`setting:${field.key}`] === 'saving'}
                disabled={locked}
                title={locked ? t('modules.lockedHint') : undefined}
                onchange={(v) => saveSetting(field, v ? 'on' : 'off')}
              >
                {#snippet status()}<SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />{/snippet}
              </SwitchRow>
            </div>
          {:else if field.type === 'timezone'}
            <div class="setting-row">
              <label class="tr-text" for="mod-setting-{field.key}">
                <Text as="span" size="sm">{fieldCopy(field, 'label')}</Text>
                {#if fieldCopy(field, 'help')}<Text as="span" size="xs" tone="muted">{fieldCopy(field, 'help')}</Text>{/if}
              </label>
              <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
              <TimezonePicker id="mod-setting-{field.key}" value={config[field.key] ?? ''} zones={tzZones} disabled={locked} onPick={(tz) => saveSetting(field, tz)} />
            </div>
            {#if browserZone && (config[field.key] ?? '') !== browserZone}
              <div class="tz-suggest">
                <AlertBanner tone="success" role="status" stack>
                  {t('modules.tzSuggested', { tz: browserZone })}
                  {#snippet actions()}
                    <Button variant="secondary" size="sm" disabled={locked} onclick={() => saveSetting(field, browserZone)} tone="success">{t('modules.tzApply', { tz: browserZone })}</Button>
                  {/snippet}
                </AlertBanner>
              </div>
            {/if}
          {:else}
            <div class="setting-row {field.type === 'textarea' ? 'stacked' : ''}">
              <label class="tr-text" for="mod-setting-{field.key}">
                <Text as="span" size="sm">{fieldCopy(field, 'label')}</Text>
                {#if fieldCopy(field, 'help')}<Text as="span" size="xs" tone="muted">{fieldCopy(field, 'help')}</Text>{/if}
              </label>
              <SaveStatus state={modStatus[`setting:${field.key}`] ?? 'idle'} />
              {#if field.type === 'select'}
                <div class="setting-picker">
                  <Select
                    fill
                    disabled={locked}
                    id="mod-setting-{field.key}"
                    value={config[field.key] || field.placeholder || field.options?.[0]?.value || ''}
                    onchange={(e) => saveSetting(field, e.currentTarget.value)}
                    options={(field.options ?? []).map((opt) => ({ value: opt.value, label: tModuleFieldOption(t, def.id, field.key, opt) }))}
                  />
                </div>
              {:else if field.type === 'textarea'}
                <Textarea
                  id="mod-setting-{field.key}"
                  fill
                  rows={6}
                  placeholder={fieldCopy(field, 'placeholder')}
                  value={config[field.key] ?? ''}
                  disabled={locked}
                  onchange={(e: Event & { currentTarget: HTMLTextAreaElement }) => saveSetting(field, e.currentTarget.value)}
                />
              {:else}
                <Input
                  id="mod-setting-{field.key}"
                  type={field.type === 'number' ? 'number' : 'text'}
                  disabled={locked}
                  placeholder={fieldCopy(field, 'placeholder')}
                  value={config[field.key] ?? ''}
                  onchange={(e: Event & { currentTarget: HTMLInputElement }) => saveSetting(field, e.currentTarget.value)}
                  onkeydown={blurOnEnter}
                />
              {/if}
            </div>
          {/if}
        {/each}
      </Card>
    </div>
  {/if}

  {#if hasDeck}
    <DeckLayout inspecting={editing && hasInspector}>
      <DeckList>
        {#if isTriggers}
          <div class="section-head rules-head anchor" id="mod-replies" tabindex="-1">
            <div class="rh-text">
              <Heading level={6} as="h2" variant="eyebrow">{t('modules.triggerRulesTitle')}</Heading>
              <Text as="span" size="xs" tone="muted">{t('modules.triggerRulesHint')}</Text>
            </div>
            {#if locked}<span class="lock-hint"><Tag tone="quiet">{t('modules.betaPremium')}</Tag></span>{/if}
            <Button variant="ghost" onclick={addRule} disabled={locked}>{t('modules.addTrigger')}</Button>
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
                    disabled={locked}
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
          <div class="section-head rules-head anchor" id="mod-replies" tabindex="-1">
            <div class="rh-text"><Heading level={6} as="h2" variant="eyebrow">{t('modules.repliesLabel')}</Heading></div>
            {#if locked}<span class="lock-hint"><Tag tone="quiet">{t('modules.betaPremium')}</Tag></span>{/if}
            {#if showReplyFilter}
              <div class="reply-filter">
                <SearchInput bind:value={replyQuery} placeholder={t('modules.replyFilterPh')} aria-label={t('modules.replyFilterLabel')} clearLabel={t('modules.searchClear')} autocomplete="off" fill />
              </div>
            {/if}
          </div>
          <ul class="list" aria-label={t('modules.repliesLabel')}>
            {#each visibleReplies as { reply, i } (reply.key)}
              <li>
                <ReplyRow
                  moduleId={def.id}
                  {reply}
                  message={config[reply.messageKey] ?? ''}
                  index={i + 1}
                  status={modStatus[reply.key] ?? 'idle'}
                  expanded={expanded === reply.key}
                  enabled={reply.enableKey ? replyOn(reply) : undefined}
                  disabled={locked}
                  onExpand={() => openReply(reply)}
                  onToggle={() => toggleReply(reply)}
                />
              </li>
            {/each}
          </ul>
          {#if visibleReplies.length === 0}
            <div class="reply-none"><Text size="sm" tone="muted">{t('modules.replyFilterNone')}</Text></div>
          {/if}
        {/if}

        <ModuleCommandList sectionId="mod-commands" moduleId={def.id} commands={def.commands ?? []} />
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
                  onDelete={askDeleteRule}
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
    </DeckLayout>
  {/if}
</section>

<ConfirmDialog
  open={deleteIndex !== null}
  title={t('modules.deleteRuleTitle')}
  body={t('modules.deleteRuleBody', { phrase: deletePhrase })}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  danger
  onCancel={() => (deleteIndex = null)}
  onConfirm={confirmDeleteRule}
/>

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
  }
  .crumb-sep { opacity: 0.5; }

  .settings-card { margin-bottom: 16px; }
  .master-row { display: flex; align-items: center; gap: 12px; padding: 16px 18px; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }

  .disabled-note {
    padding: 12px 18px 16px;
    border-top: 1px solid var(--bb-border);
  }

  .section-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--bb-border);
  }

  .setting-row {
    --input-w: min(260px, 44vw);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    border-top: 1px solid var(--bb-border);
  }
  .setting-picker { width: min(260px, 44vw); }

  .setting-row.toggle { display: block; }
  .setting-row.stacked { flex-direction: column; align-items: stretch; }
  .setting-row.stacked .tr-text { margin-right: 0; }
  @media (max-width: 560px) {
    .setting-row { --input-w: 100%; flex-wrap: wrap; }
    .setting-picker { width: 100%; }
  }

  .tz-suggest {
    padding: 0 18px;
  }

  .list { list-style: none; margin: 0; padding: 0; }

  .anchor { scroll-margin-top: calc(58px + env(safe-area-inset-top, 0px) + 56px); }
  .anchor:focus { outline: none; }
  .lock-hint { flex: none; }
  .autosave { margin-left: auto; }
  .reply-filter { width: min(260px, 44vw); flex: none; }
  .reply-none { padding: 16px 18px; }
  .rh-text { display: flex; flex-direction: column; gap: 2px; margin-right: auto; min-width: 0; }
</style>
