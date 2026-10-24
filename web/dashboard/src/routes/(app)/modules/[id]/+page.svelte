<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { namespaceReplyTemplate, type ActionOk } from '@bagel/kit';
  import { postAction, reseedOnChange } from '$lib/client-actions.svelte';
  import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SectionNav from '@bagel/ui/svelte/SectionNav.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n, automodToggleDefault, moduleDef, tModuleLabel, tModuleDescription, tModuleFieldPart, tModuleFieldOption, tModuleReplyPart, type ModuleCommandInfo, type ModuleField, type ModuleReply, type Perm, MOD } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import ReplyRow from '$lib/components/modules/ReplyRow.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import ReplyEditor from '$lib/components/modules/ReplyEditor.svelte';
  import ModuleCommandList from '$lib/components/modules/ModuleCommandList.svelte';
  import ModuleCommandEditor, { commandLevel } from '$lib/components/modules/ModuleCommandEditor.svelte';
  import TriggerRuleEditor from '$lib/components/modules/TriggerRuleEditor.svelte';
  import { MATCH_KEY, TriggerRules } from '$lib/components/modules/trigger-rules.svelte';
  import TimezonePicker from '$lib/components/modules/TimezonePicker.svelte';

  let { data } = $props();

  const { t } = getI18n();
  const canUpgrade = $derived(!data.delegateOf || (data.sections ?? []).includes('billing'));
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
  const rehearsable = $derived((def.commands ?? []).some((c) => !!c.preview));
  const hasInspector = $derived(isTriggers || hasReplies || rehearsable);
  const locked = $derived(data.locked === true);
  const hasDeck = $derived(hasInspector || (def.commands?.length ?? 0) > 0);

  reseedOnChange(() => data.def.id, () => {
    resetFromData();
    doClose();
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

  function resetFromData(edits?: RefetchEdits) {
    enabled = edits?.enabled ?? data.enabled;
    config = { ...data.config, ...edits?.partial };
    rev = data.revision ?? 0;
    rulesCtl.reset(config.rules ?? '');
  }

  async function reloadAfterConflict() {
    // Cancel queued edits drafted against the state being replaced.
    writeGeneration += 1;
    // Edits made during the refetch are queued against the new revision, so keep them.
    const edits: RefetchEdits = { partial: {} };
    refetchEdits = edits;
    await invalidateAll();
    refetchEdits = null;
    // The reseed effect only fires on module navigation, so refresh in place.
    resetFromData(edits);
  }

  async function runPatch(partial: Record<string, string>, en: boolean): Promise<PatchOutcome> {
    const body = new FormData();
    body.set('is_enabled', en ? 'on' : '');
    body.set('expected_rev', String(rev));
    body.set('partial', JSON.stringify(partial));
    const payload = await postAction<ActionOk & { rev?: number; conflict?: boolean }>('patch', body);
    if (payload?.ok) {
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

  async function optimisticWrite(write: {
    key: string;
    partial: Record<string, string>;
    failText: string;
    revert: () => void;
    onSaved?: () => void;
  }) {
    setStatus(write.key, 'saving');
    const outcome = await patch(write.partial, enabled);
    if (outcome === 'saved') {
      ackSaved(write.key);
      write.onSaved?.();
      return;
    }
    flagError(write.key);
    if (outcome === 'failed') {
      write.revert();
      toast('danger', write.failText);
    }
  }

  async function toggleModule() {
    const before = enabled;
    enabled = !enabled;
    await optimisticWrite({
      key: 'module',
      partial: {},
      failText: t('modules.couldNotToggle', { label: modLabel }),
      revert: () => (enabled = before)
    });
  }

  async function saveSetting(field: ModuleField, value: string) {
    await saveConfigValue(field.key, value);
  }

  async function saveConfigValue(key: string, value: string) {
    const before = config[key] ?? '';
    if (value.trim() === before.trim()) return;
    config = { ...config, [key]: value.trim() };
    await optimisticWrite({
      key: `setting:${key}`,
      partial: { [key]: value.trim() },
      failText: t('modules.saveFailed'),
      revert: () => (config = { ...config, [key]: before })
    });
  }

  function settingToggleOn(field: ModuleField): boolean {
    const v = config[field.key] ?? '';
    if (v === 'on') return true;
    if (v === 'off') return false;
    if (field.followsLevel) return automodToggleDefault(config['level'] || 'moderate', field.key);
    return field.defaultOn === true;
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
    const next = was ? 'off' : 'on';
    config = { ...config, [key]: next };
    await optimisticWrite({
      key: reply.key,
      partial: { [key]: next },
      failText: t('modules.couldNotToggle', { label: tModuleReplyPart(t, def.id, reply, 'label') }),
      revert: () => (config = { ...config, [key]: was ? 'on' : 'off' })
    });
  }

  let expanded = $state<string | null>(null);
  let editMessage = $state('');
  let busy = $state(false);
  const selectedReply = $derived(expanded ? def.replies.find((r) => r.key === expanded) : undefined);
  const expandedCommand = $derived(expanded?.startsWith('cmd:') ? expanded.slice(4) : null);
  const selectedCommand = $derived(expandedCommand ? def.commands?.find((c) => c.trigger === expandedCommand) : undefined);

  function commandRole(command: ModuleCommandInfo): Perm | undefined {
    return command.permKey ? commandLevel(config[command.permKey], command.perm) : undefined;
  }

  function openCommand(command: ModuleCommandInfo) {
    if (locked) return;
    if (expandedCommand === command.trigger) {
      closeInspector();
      return;
    }
    discard.guard(() => {
      expanded = `cmd:${command.trigger}`;
    });
  }

  const discard = createDiscardGuard(() => inspectorDirty);
  function doClose() {
    expanded = null;
    rulesCtl.index = null;
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
    await optimisticWrite({
      key: r.key,
      partial: { [r.messageKey]: editMessage },
      failText: t('modules.saveFailed'),
      revert: () => (config = { ...config, [r.messageKey]: prev ?? '' }),
      onSaved: () => toast('success', t('modules.saved', { label: modLabel }))
    });
    busy = false;
  }

  // svelte-ignore state_referenced_locally
  const rulesCtl = new TriggerRules(data.config.rules ?? '', {
    get expanded() {
      return expanded;
    },
    set expanded(value) {
      expanded = value;
    },
    get editMessage() {
      return editMessage;
    },
    set editMessage(value) {
      editMessage = value;
    },
    get busy() {
      return busy;
    },
    set busy(value) {
      busy = value;
    },
    get locked() {
      return locked;
    },
    text: {
      newRule: () => t('modules.newRule'),
      match: (match) => t(MATCH_KEY[match]),
      event: (rule) => t('modules.ruleEvent', { phrase: rule.phrase || '…' }),
      saveFailed: () => t('modules.saveFailed'),
      couldNotToggle: (rule) => t('modules.couldNotToggle', { label: rule.phrase }),
      saved: () => t('modules.saved', { label: modLabel })
    },
    guard: discard.guard,
    closeInspector,
    doClose,
    setSaving: (key) => setStatus(key, 'saving'),
    ackSaved,
    flagError,
    persist: async (serialized) => {
      const outcome = await patch({ rules: serialized }, enabled);
      if (outcome === 'saved') config = { ...config, rules: serialized };
      return outcome;
    }
  });
  const ruleRows: ModuleReply[] = $derived(rulesCtl.rows());

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
    if (isTriggers) return rulesCtl.dirty();
    if (selectedReply) return editMessage !== namespaceReplyTemplate(def.id, selectedReply, config[selectedReply.messageKey] ?? '');
    return false;
  });

  const editing = $derived(!!expanded);
  const inspectorTitle = $derived(
    isTriggers ? (rulesCtl.index === -1 ? t('modules.newTrigger') : t('modules.editTrigger')) : selectedReply ? tModuleReplyPart(t, def.id, selectedReply, 'label') : (selectedCommand?.trigger ?? t('modules.inspector'))
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

  function requiredDef(field: ModuleField) {
    return field.requires && (data.requiresOff ?? []).includes(field.requires) ? moduleDef(field.requires) : undefined;
  }

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
      {t('modules.betaLockedBody')}{#if !canUpgrade} {t('modules.betaAskOwner', { login: data.delegateLogin ?? '' })}{/if}
      {#snippet actions()}
        {#if canUpgrade}
          <ButtonLink href="/billing" variant="ghost">{t('modules.betaUpgrade')}</ButtonLink>
        {/if}
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
            busy={modStatus['module'] === 'saving'}
            onCheckedChange={toggleModule}
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
          {@const needs = requiredDef(field)}
          {#if field.type === 'toggle'}
            <div class="setting-row toggle">
              <SwitchRow
                control="end"
                label={fieldCopy(field, 'label')}
                hint={fieldCopy(field, 'help') || undefined}
                hintId="sh-{field.key}"
                checked={settingToggleOn(field)}
                busy={modStatus[`setting:${field.key}`] === 'saving'}
                disabled={locked || !!needs}
                title={locked ? t('modules.lockedHint') : undefined}
                onCheckedChange={(v) => saveSetting(field, v ? 'on' : 'off')}
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
                  disabled={locked || !!needs}
                  placeholder={fieldCopy(field, 'placeholder')}
                  value={config[field.key] ?? ''}
                  onchange={(e: Event & { currentTarget: HTMLInputElement }) => saveSetting(field, e.currentTarget.value)}
                  onkeydown={blurOnEnter}
                />
              {/if}
            </div>
          {/if}
          {#if needs}
            <div class="disabled-note">
              <AlertBanner tone="warning" stack>
                {t('modules.settingNeeds', { module: tModuleLabel(t, needs) })}
                {#snippet actions()}
                  <ButtonLink href={needs.href ?? `/modules/${needs.id}`} variant="ghost">{t('modules.nestedUnderLink', { parent: tModuleLabel(t, needs) })}</ButtonLink>
                {/snippet}
              </AlertBanner>
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
            <Button variant="ghost" onclick={() => rulesCtl.add()} disabled={locked}>{t('modules.addTrigger')}</Button>
          </div>
          {#if ruleRows.length}
            <ul class="list" aria-label={t('modules.triggerRulesTitle')}>
              {#each ruleRows as reply, i (reply.key)}
                <li>
                  <ReplyRow
                    moduleId={def.id}
                    {reply}
                    message={rulesCtl.rules[i].response}
                    index={i + 1}
                    status={modStatus[reply.key] ?? 'idle'}
                    expanded={expanded === reply.key}
                    enabled={rulesCtl.rules[i].enabled}
                    disabled={locked}
                    onExpand={() => rulesCtl.open(i)}
                    onToggle={() => rulesCtl.toggle(i)}
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

        <ModuleCommandList
          sectionId="mod-commands"
          moduleId={def.id}
          commands={def.commands ?? []}
          expanded={expandedCommand}
          roleOf={commandRole}
          onExpand={rehearsable ? openCommand : undefined}
        />
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
                  bind:phrase={rulesCtl.phrase}
                  bind:match={rulesCtl.match}
                  bind:message={editMessage}
                  {busy}
                  isNew={rulesCtl.index === -1}
                  onSave={() => rulesCtl.save()}
                  onCancel={closeInspector}
                  onDelete={() => rulesCtl.askDelete()}
                />
              {/key}
            </Scroller>
          {:else if selectedReply}
            <Scroller fill padding="16px" smooth>
              {#key selectedReply.key}
                <ReplyEditor moduleId={def.id} reply={selectedReply} bind:message={editMessage} {busy} onCancel={closeInspector} onSave={saveReply} />
              {/key}
            </Scroller>
          {:else if selectedCommand}
            <Scroller fill padding="16px" smooth>
              <ModuleCommandEditor
                moduleId={def.id}
                command={selectedCommand}
                role={commandRole(selectedCommand)}
                status={modStatus[`setting:${selectedCommand.permKey}`] ?? 'idle'}
                disabled={locked}
                onRole={(role) => saveConfigValue(selectedCommand.permKey ?? '', role)}
              />
            </Scroller>
          {/if}
        </InspectorSurface>
      {/if}
    </DeckLayout>
  {/if}
</section>

<ConfirmDialog
  open={rulesCtl.deleteIndex !== null}
  title={t('modules.deleteRuleTitle')}
  body={t('modules.deleteRuleBody', { phrase: rulesCtl.deletePhrase })}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  onCancel={() => (rulesCtl.deleteIndex = null)}
  onConfirm={() => rulesCtl.confirmDelete()}
  tone="danger"
/>

<ConfirmDialog
  open={discard.open}
  title={t('modules.discardTitle')}
  body={t('modules.discardBody')}
  confirmLabel={t('modules.discard')}
  cancelLabel={t('modules.keepEditing')}
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
  tone="danger"
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
