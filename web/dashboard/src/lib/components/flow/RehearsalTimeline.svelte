<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import { activeRandomPaths } from '@bagel/kit/automations';
  import type { RehearsalOverrides, RehearsalScenario } from '#lib/flow/rehearse';
  import type { Snippet } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import FlowIcon from './FlowIcon.svelte';
  import RunOutcome from './RunOutcome.svelte';
  import RehearsedLineRow from '#lib/components/commands/RehearsedLineRow.svelte';
  import '@bagel/ui/styles/elements/chat-preview.css';
  import { setStoredValue, type RehearsalStep } from '#lib/flow/rehearse';
  import { randomPathLabel } from '#lib/flow/copy';
  import { rehearsalTimeline, stepOutcome } from '#lib/flow/rehearsal-view';
  import { specFor } from '#lib/flow/model';

  let { steps, scenario, overrides, summary, verifyRepair, onScenario, onOverride, onSelect, event }: {
    steps: RehearsalStep[];
    scenario: RehearsalScenario;
    overrides: RehearsalOverrides;
    summary: string;
    verifyRepair?: (patch: Partial<RehearsalScenario>, path?: string) => boolean;
    onScenario: (patch: Partial<RehearsalScenario>) => void;
    onOverride: (key: string, value: string) => void;
    onSelect: (id: string, fieldKey?: string) => void;
    event: Snippet;
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
  const yesNo = $derived([{ value: 'yes', label: t('flows.test.yes') }, { value: 'no', label: t('flows.test.no') }]);
  const choices = $derived({
    'cond.chance': yesNo,
    'cond.cooldown': [{ value: 'ready', label: t('flows.test.ready') }, { value: 'cooling', label: t('flows.test.cooling') }],
    'reply.http_get': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }],
    'reply.http_post': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }],
    'reply.run': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.recipeFailed') }],
    'cond.category': yesNo,
    'cond.follow': yesNo,
    'cond.accountAge': yesNo
  } as Record<string, { value: string; label: string }[]>);
  const defaults: Record<string, Record<string, string>> = { 'cond.chance': { yes: 'yes', no: 'no' }, 'cond.cooldown': { yes: 'ready', no: 'cooling' } };
  const pretendYes = new Set(['cond.category', 'cond.follow', 'cond.accountAge']);
  const initial = (step: RehearsalStep) => {
    const type = step.node?.type ?? '';
    if (pretendYes.has(type)) return 'yes';
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
  const stepIcon = (step: RehearsalStep) => specFor(step.node?.type ?? '')?.icon ?? (step.kind === 'wait' ? 'clock' : 'start');
  type Row = ReturnType<typeof rehearsalTimeline>[number]['rows'][number];
  function contextLine(row: Row, startsAt: number | null): string {
    const { step } = row;
    const lane = step.lane.length ? t('flows.test.lane', { lane: step.lane.map((value) => value + 1).join('.') }) : '';
    const pass = row.pass ? t('flows.test.pass', { pass: row.pass, total: row.total }) : '';
    const time = startsAt ? `+${startsAt / 1000}s` : '';
    return [time, lane, pass, step.sample ? t('flows.test.sample') : ''].filter(Boolean).join(' · ');
  }
  const fixField = (step: RehearsalStep) => step.issues?.find((issue) => issue.fieldKey)?.fieldKey;
</script>

<ol class="cards" aria-label={t('flows.test.timeline')}>
  {#each visibleGroups as group (group.atMs)}
    {#each group.rows as row, index (row.step.key)}
      {@const step = row.step}
      {@const pretend = choicesFor(step)}
      {@const api = step.node?.type.startsWith('reply.http_')}
      {@const roleNote = step.node?.type === 'cond.role' ? step.note : undefined}
      {@const context = contextLine(row, index === 0 ? group.atMs : null)}
      <li class="run-step" style:margin-inline-start="{laneOffset(step)}px">
        <Card class={step.status === 'skipped' ? 'run-card run-card--quiet' : 'run-card'}>
          <div class="card-head">
            <IconTile style="--tile: var(--bb-space-6)"><FlowIcon name={stepIcon(step)} size={16} /></IconTile>
            <Heading level={3} variant="card" class="run-title">
              <button class="select bb-fit-box" type="button" disabled={!step.id} onclick={() => step.id && onSelect(step.id)}><bb-fit>{step.label}</bb-fit></button>
            </Heading>
            <RunOutcome outcome={stepOutcome(step)} />
          </div>
          {#if context}<Text as="span" size="xs" tone="muted" class="run-context">{context}</Text>{/if}
          {#if step.kind === 'trigger'}
            <Text size="sm" tone="muted-light" class="run-text bb-fit-box"><bb-fit mode="block">{summary}</bb-fit></Text>
            <div class="event">{@render event()}</div>
          {:else if step.text}
            <Text size="sm" tone="muted-light" class="run-text bb-fit-box"><bb-fit mode="block">{step.text}</bb-fit></Text>
          {/if}
          {#if step.secondary}<Text as="span" size="xs" tone="muted" class="run-context">{step.secondary}</Text>{/if}
          {#if step.kind === 'say' && step.chatLines.length}
            <div class="bubble">
              {#each step.chatLines as line, lineIndex (lineIndex)}
                <RehearsedLineRow {line} seed="ItsBagelBot" />
              {/each}
            </div>
          {/if}
          {#if roleNote}<Text size="sm" tone="warning" class="run-role bb-fit-box"><bb-fit mode="block">{roleNote}</bb-fit></Text>{/if}
          {#if step.note && !roleNote}<Text size="sm" tone="warning" class="run-note">{step.note}</Text>{/if}
          {#each step.missing as field (field)}<Text size="sm" tone="warning" class="run-note">{t('flows.issue.requiredField', { field })}</Text>{/each}
          {#if pretend && step.id}
            <div class="pretend">
              <Label as="span" class="pretend-label bb-fit-box"><bb-fit>{t('flows.test.pretendStep')}</bb-fit></Label>
              <SegmentedControl class="bb-tabs--even bb-tabs--plain" label={t('flows.test.pretendStep')} options={pretendOptions(step, pretend)} value={pretendValue(step)} onValueChange={(value) => choose(step, value)} />
            </div>
          {/if}
          {#if api && step.id}
            {@const failed = pretendValue(step) === 'failure'}
            {@const key = `${step.id}.${failed ? 'error' : 'response'}`}
            <Field class="run-field" label={t(failed ? 'flows.test.errorCode' : 'flows.test.response')} for="{uid}-{step.key}-response">
              <Input id="{uid}-{step.key}-response" fill placeholder={t('flows.test.notSet')} value={overrides[key] ?? (failed ? 'upstream_error' : '')} oninput={(event) => onOverride(key, event.currentTarget.value)} />
            </Field>
          {/if}
          {#if step.node?.type === 'cond.var'}
            <Field class="run-field" label={t('flows.test.stored', { name: String(step.node.config.name || t('flows.test.notSet')) })} for="{uid}-{step.key}-stored">
              <Input id="{uid}-{step.key}-stored" fill placeholder={t('flows.test.notSet')} value={stored(step)} oninput={(event) => step.node && onScenario(setStoredValue(scenario, step.node, event.currentTarget.value))} />
            </Field>
          {/if}
          {#if step.issues}
            <div class="action">
              <Button variant="secondary" size="sm" onclick={() => step.id && onSelect(step.id, fixField(step))}><bb-fit>{t('flows.test.fixStep')}</bb-fit></Button>
            </div>
          {:else if step.kind === 'check' && !pretend}
            <div class="action" class:off={!step.repair} inert={!step.repair}>
              <Button variant="secondary" size="sm" disabled={!step.repair} onclick={() => applyRepair(step)}><bb-fit>{t('flows.test.makePass')}</bb-fit></Button>
              <Text as="span" size="xs" tone="warning" class="run-rejected" role="status">{rejected === step.key ? t('flows.test.repairBlocked') : ''}</Text>
            </div>
          {/if}
          {#if row.more}<Text as="span" size="xs" tone="muted" class="run-context">{t('flows.test.moreRows', { count: row.more })}</Text>{/if}
        </Card>
      </li>
    {/each}
  {/each}
</ol>
<div class="more" class:off={shown >= totalRows} inert={shown >= totalRows}>
  <Button variant="ghost" size="sm" onclick={() => shown += 40}><bb-fit>{t('flows.test.moreRows', { count: Math.max(0, totalRows - shown) })}</bb-fit></Button>
</div>

<style>
  .cards {
    display: flex;
    flex: none;
    flex-direction: column;
    gap: var(--bb-space-5);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .run-step { position: relative; min-width: 0; }
  .run-step + .run-step::before {
    content: '';
    position: absolute;
    inset-inline-start: 50%;
    bottom: 100%;
    width: 1px;
    height: var(--bb-space-5);
    background: var(--bb-rule-strong);
  }
  .cards :global(.run-card) {
    --card-pad: var(--bb-space-4);
    --card-bg: var(--bb-elev-2);
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--bb-space-3);
  }
  .cards :global(.run-card--quiet) { --card-bg: var(--bb-elev-1); }
  .card-head {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-2);
  }
  .card-head :global(.run-title) {
    min-width: 0;
    margin: 0;
    font: 600 var(--bb-text-md)/1.25 var(--bb-font-body);
    letter-spacing: normal;
  }
  .select {
    display: block;
    width: 100%;
    height: var(--bb-space-5);
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: start;
    cursor: pointer;
  }
  .select:disabled { cursor: default; }
  .select:not(:disabled):hover { color: var(--bb-green-glow); }
  .select:focus-visible { outline: 2px solid var(--bb-focus); outline-offset: var(--bb-space-1); border-radius: var(--bb-radius-xs); }
  .cards :global(.run-text) {
    height: calc(var(--bb-space-5) + var(--bb-space-4));
    margin: 0;
    overflow-wrap: anywhere;
  }
  .cards :global(.run-context),
  .cards :global(.run-note) { margin: 0; overflow-wrap: anywhere; }
  .cards :global(.run-role) { height: var(--bb-space-8); margin: 0; }
  .cards :global(.run-field) { --field-mb: 0; }
  .event {
    padding-block-start: var(--bb-space-3);
    border-block-start: 1px solid var(--bb-rule);
  }
  .bubble { min-width: 0; white-space: pre-line; }
  .pretend {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .pretend :global(.pretend-label) { flex: 0 1 var(--bb-space-9); min-width: 0; height: var(--bb-space-4); }
  .pretend > :global(:last-child) { flex: 1; min-width: 0; }
  .action {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--bb-space-1);
    min-height: calc(var(--bb-space-6) + var(--bb-space-2));
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .action :global(.run-rejected) { min-height: var(--bb-space-4); }
  .more { flex: none; min-height: var(--bb-space-6); }
  .off { visibility: hidden; opacity: 0; }
  @media (prefers-reduced-motion: reduce) { .action { transition: none; } }
</style>
