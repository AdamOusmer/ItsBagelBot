<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import { activeRandomPaths } from '@bagel/kit/automations';
  import type { RehearsalOverrides, RehearsalScenario } from '#lib/flow/rehearse';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import RehearsedLineRow from '#lib/components/commands/RehearsedLineRow.svelte';
  import '@bagel/ui/styles/elements/chat-preview.css';
  import { setStoredValue, type RehearsalStep } from '#lib/flow/rehearse';
  import { randomPathLabel } from '#lib/flow/copy';
  import { rehearsalTimeline } from '#lib/flow/rehearsal-view';

  let { steps, scenario, overrides, verifyRepair, onScenario, onOverride, onSelect }: {
    steps: RehearsalStep[];
    scenario: RehearsalScenario;
    overrides: RehearsalOverrides;
    verifyRepair?: (patch: Partial<RehearsalScenario>, path?: string) => boolean;
    onScenario: (patch: Partial<RehearsalScenario>) => void;
    onOverride: (key: string, value: string) => void;
    onSelect: (id: string, fieldKey?: string) => void;
  } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  let rejected = $state<string | null>(null);
  function applyRepair(step: RehearsalStep) {
    if (!step.repair) return;
    rejected = null;
    if (verifyRepair?.(step.repair, step.path)) onScenario(step.repair);
    else rejected = step.key;
  }
  const laneOffset = (step: RehearsalStep) => step.lane.reduce((offset, lane) => offset + lane + 1, 0) * 10;
  const groups = $derived(rehearsalTimeline(steps));
  let shown = $state(40);
  const totalRows = $derived(groups.reduce((sum, group) => sum + group.rows.length, 0));
  const visibleGroups = $derived.by(() => {
    let remaining = shown;
    return groups.map((group) => {
      const rows = group.rows.slice(0, Math.max(0, remaining));
      remaining -= rows.length;
      return { ...group, rows };
    }).filter((group) => group.rows.length > 0);
  });
  const choices = $derived({
    'cond.chance': [{ value: 'yes', label: t('flows.test.yes') }, { value: 'no', label: t('flows.test.no') }],
    'cond.cooldown': [{ value: 'ready', label: t('flows.test.ready') }, { value: 'cooling', label: t('flows.test.cooling') }],
    'reply.http_get': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }],
    'reply.http_post': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }],
    'reply.run': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.recipeFailed') }]
  } as Record<string, { value: string; label: string }[]>);
  const defaults: Record<string, Record<string, string>> = { 'cond.chance': { yes: 'yes', no: 'no' }, 'cond.cooldown': { yes: 'ready', no: 'cooling' } };
  const initial = (step: RehearsalStep) => {
    const type = step.node?.type ?? '';
    if (type.startsWith('reply.http_')) return 'ok';
    return defaults[type]?.[step.port ?? ''] ?? step.port ?? 'ok';
  };

  function blockedChance(step: RehearsalStep, choice: string): boolean {
    if (step.node?.type !== 'cond.chance') return false;
    const percent = Number(step.node.config.percent);
    return choice === 'no' ? percent >= 100 : percent <= 0;
  }

  const pretendOptions = (step: RehearsalStep, options: { value: string; label: string }[]) => options.map((option) => ({
    ...option, attrs: blockedChance(step, option.value) ? { disabled: '' } : undefined
  }));
  const pretendValue = (step: RehearsalStep) => {
    if (step.node?.type === 'reply.random') return step.detail?.path ?? '1';
    const value = overrides[step.id ?? ''] ?? initial(step);
    return blockedChance(step, value) ? initial(step) : value;
  };
  function choicesFor(step: RehearsalStep) {
    if (step.node?.type === 'reply.random') {
      return activeRandomPaths(step.node.config).map(({ path }) => ({ value: String(path), label: randomPathLabel(t, path) }));
    }
    return choices[step.node?.type ?? ''];
  }
  function choose(step: RehearsalStep, value: string) {
    if (!step.id) return;
    const key = step.node?.type === 'reply.random' ? `${step.id}.path` : step.id;
    onOverride(key, value);
  }
  const stored = (step: RehearsalStep) => {
    const key = step.node?.config.scope === 'viewer' ? 'viewerVars' : 'channelVars';
    return scenario.state?.[key]?.[String(step.node?.config.name ?? '').toLowerCase()] ?? '';
  };
  const status = (step: RehearsalStep) => {
    if (step.detail?.code === 'child_failed') return t('flows.test.recipeFailed');
    return t(step.status === 'yes' ? 'flows.test.passed' : 'flows.test.failed');
  };
</script>

<ol class="timeline" aria-label={t('flows.test.timeline')}>
  {#each visibleGroups as group (group.atMs)}
    <li class="group">
      <span class="time">{group.atMs === 0 ? '0s' : `+${group.atMs / 1000}s`}</span>
      <div class="rows">
        {#each group.rows as row (row.step.key)}
          {@const step = row.step}
          {@const pretend = choicesFor(step)}
          {@const api = step.node?.type.startsWith('reply.http_')}
          {@const roleNote = step.node?.type === 'cond.role' ? step.note : undefined}
          {@const showResult = step.kind === 'check' || step.issues || step.detail?.code === 'child_failed'}
          <article class="row" class:muted={step.status === 'skipped'} style:margin-inline-start="{laneOffset(step)}px">
            {#if step.lane.length}
              <span class="context">{t('flows.test.lane', { lane: step.lane.map((value) => value + 1).join('.') })}</span>
            {/if}
            {#if row.pass}<span class="context">{t('flows.test.pass', { pass: row.pass, total: row.total })}</span>{/if}
            <div class="heading">
              <button class="select" type="button" disabled={!step.id} onclick={() => step.id && onSelect(step.id)}>
                <span class="label bb-fit-box"><bb-fit>{step.label}</bb-fit></span>
                {#if step.text}<span class="detail bb-fit-box"><bb-fit mode="block">{step.text}</bb-fit></span>{/if}
                {#if step.secondary}<span class="context">{step.secondary}</span>{/if}
              </button>
              <div class="fix" class:fix-hidden={!step.issues} inert={!step.issues}>
                <Button variant="ghost" size="sm" block onclick={() => step.id && onSelect(step.id, step.issues?.find((issue) => issue.fieldKey)?.fieldKey)}>
                  <bb-fit>{t('flows.test.fixStep')}</bb-fit>
                </Button>
              </div>
            </div>
            {#if step.kind === 'say'}
              <div class="bubble">
                {#each step.chatLines as line, index (index)}
                  <RehearsedLineRow {line} seed="ItsBagelBot" />
                {/each}
              </div>
            {/if}
            {#if showResult}<span class="result">{status(step)}</span>{/if}
            {#if step.sample}<span class="context">{t('flows.test.sample')}</span>{/if}
            {#if roleNote}<span class="missing role-note bb-fit-box"><bb-fit mode="block">{roleNote}</bb-fit></span>{/if}
            {#if step.note && !roleNote}<span class="missing">{step.note}</span>{/if}
            {#each step.missing as field (field)}<span class="missing">{t('flows.issue.requiredField', { field })}</span>{/each}
            {#if pretend && step.id}
              <div class="pretend">
                <span class="context">{t('flows.test.pretendStep')}</span>
                <SegmentedControl label={t('flows.test.pretendStep')} options={pretendOptions(step, pretend)} value={pretendValue(step)} onValueChange={(value) => choose(step, value)} />
              </div>
            {/if}
            {#if api && step.id}
              {@const failed = pretendValue(step) === 'failure'}
              {@const key = `${step.id}.${failed ? 'error' : 'response'}`}
              <Field label={t(failed ? 'flows.test.errorCode' : 'flows.test.response')} for="{uid}-{step.key}-response">
                <Input id="{uid}-{step.key}-response" fill placeholder={t('flows.test.notSet')} value={overrides[key] ?? (failed ? 'upstream_error' : '')} oninput={(event) => onOverride(key, event.currentTarget.value)} />
              </Field>
            {/if}
            {#if step.node?.type === 'cond.var'}
              <Field label={t('flows.test.stored', { name: String(step.node.config.name || t('flows.test.notSet')) })} for="{uid}-{step.key}-stored">
                <Input id="{uid}-{step.key}-stored" fill placeholder={t('flows.test.notSet')} value={stored(step)} oninput={(event) => step.node && onScenario(setStoredValue(scenario, step.node, event.currentTarget.value))} />
              </Field>
            {/if}
            {#if step.kind === 'check' && !pretend}
              <div class="repair" class:invisible={!step.repair} inert={!step.repair}>
                <Button variant="ghost" size="sm" onclick={() => applyRepair(step)}><bb-fit>{t('flows.test.makePass')}</bb-fit></Button>
              </div>
              <span class="missing" class:invisible={rejected !== step.key} role="status">{rejected === step.key ? t('flows.test.repairBlocked') : ''}</span>
            {/if}
            {#if row.more}<span class="context">{t('flows.test.moreRows', { count: row.more })}</span>{/if}
          </article>
        {/each}
      </div>
    </li>
  {/each}
</ol>
<div class="more" class:invisible={shown >= totalRows} inert={shown >= totalRows}>
  <Button variant="ghost" size="sm" onclick={() => shown += 40}><bb-fit>{t('flows.test.moreRows', { count: Math.max(0, totalRows - shown) })}</bb-fit></Button>
</div>

<style>
  .timeline { flex: none; margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 20px; }
  .more { flex: none; min-height: 32px; }
  .group { display: flex; align-items: flex-start; gap: 10px; }
  .time { flex: 0 0 36px; padding-top: 8px; font-size: 12px; font-variant-numeric: tabular-nums; color: var(--bb-muted-light); }
  .rows { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
  .row { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .heading { --fix-width: min(116px, 50%); display: grid; grid-template-areas: "heading"; grid-template-columns: minmax(0, 1fr); }
  .select, .fix { grid-area: heading; }
  .fix { z-index: 1; align-self: start; justify-self: end; width: var(--fix-width); transition: opacity 140ms ease; }
  .fix-hidden { opacity: 0; }
  .select { display: flex; flex-direction: column; gap: 4px; width: 100%; padding: 6px 0; border: 0; background: transparent; color: inherit; text-align: start; cursor: pointer; font: inherit; }
  .select:disabled { cursor: default; }
  .select:hover { color: var(--bb-green-glow); }
  .select:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 3px; border-radius: var(--bb-radius-xs); }
  .label { width: calc(100% - var(--fix-width) - 4px); height: 22px; font-size: 15px; font-weight: 600; }
  .detail, .result, .missing { font-size: 14px; line-height: 1.45; overflow-wrap: anywhere; }
  .detail { width: 100%; height: 42px; }
  .context { font-size: 12px; line-height: 1.4; color: var(--bb-muted-light); }
  .result { font-weight: 600; }
  .muted { opacity: 0.6; }
  .bubble { min-width: 0; white-space: pre-line; }
  .missing { color: var(--bb-tan); }
  .role-note { flex: none; height: 100px; }
  .pretend { display: flex; flex-direction: column; gap: 4px; }
  .repair { min-height: 32px; }
  .invisible { visibility: hidden; opacity: 0; }
  @media (prefers-reduced-motion: reduce) { .fix { transition: none; } }
</style>
