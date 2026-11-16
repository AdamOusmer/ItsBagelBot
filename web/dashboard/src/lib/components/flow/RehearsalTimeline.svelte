<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import type { RehearsalOverrides, RehearsalScenario } from '#lib/flow/rehearse';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import ChatBotName from '#lib/components/commands/ChatBotName.svelte';
  import '@bagel/ui/styles/elements/chat-preview.css';
  import { setStoredValue, type RehearsalStep } from '#lib/flow/rehearse';

  let { steps, scenario, overrides, onScenario, onOverride, onSelect }: {
    steps: RehearsalStep[];
    scenario: RehearsalScenario;
    overrides: RehearsalOverrides;
    onScenario: (patch: Partial<RehearsalScenario>) => void;
    onOverride: (key: string, value: string) => void;
    onSelect: (id: string) => void;
  } = $props();
  const { t } = getI18n();
  const uid = $props.id();
  type Row = { step: RehearsalStep; more: number; pass: number; total: number };
  const loopPath = (step: RehearsalStep) => step.iteration.length ? step.path.slice(0, step.path.lastIndexOf('.params.actions')) : '';
  const laneOffset = (step: RehearsalStep) => step.lane.reduce((offset, lane) => offset + lane + 1, 0) * 10;

  function timeline(events: RehearsalStep[]) {
    const loops = new Map(events.filter((step) => step.detail?.times).map((step) => [step.path, Number(step.detail?.times)]));
    const counts = new Map<string, Row[]>();
    const rows = events.map((step): Row => ({ step, more: 0, pass: step.iteration.at(-1) ?? 0, total: loops.get(loopPath(step)) ?? 0 }));
    for (const row of rows.filter((row) => row.pass)) {
      const key = loopPath(row.step);
      counts.set(key, [...(counts.get(key) ?? []), row]);
    }
    const hidden = new Set<Row>();
    for (const entries of counts.values()) {
      entries.slice(20).forEach((row) => hidden.add(row));
      if (entries.length > 20) entries[19]!.more = entries.length - 20;
    }
    const groups = new Map<number, Row[]>();
    for (const row of rows.filter((row) => !hidden.has(row))) groups.set(row.step.atMs, [...(groups.get(row.step.atMs) ?? []), row]);
    return [...groups].map(([atMs, rows]) => ({ atMs, rows }));
  }

  const groups = $derived(timeline(steps));
  const choices = $derived({
    'cond.chance': [{ value: 'yes', label: t('flows.test.yes') }, { value: 'no', label: t('flows.test.no') }],
    'cond.cooldown': [{ value: 'ready', label: t('flows.test.ready') }, { value: 'cooling', label: t('flows.test.cooling') }],
    'reply.http_get': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }],
    'reply.http_post': [{ value: 'ok', label: t('flows.test.worked') }, { value: 'failure', label: t('flows.test.apiFailed') }]
  } as Record<string, { value: string; label: string }[]>);
  const defaults: Record<string, Record<string, string>> = { 'cond.chance': { yes: 'yes', no: 'no' }, 'cond.cooldown': { yes: 'ready', no: 'cooling' } };
  const initial = (step: RehearsalStep) => defaults[step.node?.type ?? '']?.[step.port ?? ''] ?? step.port ?? 'ok';

  function blockedChance(step: RehearsalStep, choice: string): boolean {
    if (step.node?.type !== 'cond.chance') return false;
    const percent = Number(step.node.config.percent);
    return choice === 'no' ? percent >= 100 : percent <= 0;
  }

  const pretendOptions = (step: RehearsalStep, options: { value: string; label: string }[]) => options.map((option) => ({
    ...option, attrs: blockedChance(step, option.value) ? { disabled: '' } : undefined
  }));
  const pretendValue = (step: RehearsalStep) => {
    const value = overrides[step.id ?? ''] ?? initial(step);
    return blockedChance(step, value) ? initial(step) : value;
  };
  const stored = (step: RehearsalStep) => {
    const key = step.node?.config.scope === 'viewer' ? 'viewerVars' : 'channelVars';
    return scenario.state?.[key]?.[String(step.node?.config.name ?? '').toLowerCase()] ?? '';
  };
  const status = (step: RehearsalStep) => t(step.status === 'yes' ? 'flows.test.passed' : 'flows.test.failed');
</script>

<ol class="timeline" aria-label={t('flows.test.timeline')}>
  {#each groups as group (group.atMs)}
    <li class="group">
      <span class="time">{group.atMs === 0 ? '0s' : `+${group.atMs / 1000}s`}</span>
      <div class="rows">
        {#each group.rows as row (row.step.key)}
          {@const step = row.step}
          {@const pretend = choices[step.node?.type ?? '']}
          {@const api = step.node?.type.startsWith('reply.http_')}
          <article class="row" class:muted={step.status === 'skipped'} style:margin-inline-start="{laneOffset(step)}px">
            {#if step.lane.length}
              <span class="context">{t('flows.test.lane', { lane: step.lane.map((value) => value + 1).join('.') })}</span>
            {/if}
            {#if row.pass}<span class="context">{t('flows.test.pass', { pass: row.pass, total: row.total })}</span>{/if}
            <button class="select" type="button" disabled={!step.id} onclick={() => step.id && onSelect(step.id)}>
              <span class="label"><bb-fit><span data-fit>{step.label}</span></bb-fit></span>
              {#if step.text}<span class="detail"><bb-fit mode="block"><span data-fit>{step.text}</span></bb-fit></span>{/if}
              {#if step.kind === 'say'}
                <span class="bubble">
                  {#each step.lines ?? [] as line, index (index)}
                    <span class="chat-line"><ChatBotName seed="ItsBagelBot" /><span class="chat-text">{line}</span></span>
                  {/each}
                </span>
              {/if}
            </button>
            {#if step.kind === 'check'}<span class="result">{status(step)}</span>{/if}
            {#if step.sample}<span class="context">{t('flows.test.sample')}</span>{/if}
            {#if step.missing.length}<span class="missing">{t('flows.test.missing', { names: step.missing.join(', ') })}</span>{/if}
            {#if pretend && step.id}
              <div class="pretend">
                <span class="context">{t('flows.test.pretendStep')}</span>
                <SegmentedControl label={t('flows.test.pretendStep')} options={pretendOptions(step, pretend)} value={pretendValue(step)} onValueChange={(value) => step.id && onOverride(step.id, value)} />
              </div>
            {/if}
            {#if api && step.id}
              <Field label={t('flows.test.response')} for="{uid}-{step.key}-response">
                <Input id="{uid}-{step.key}-response" fill placeholder={t('flows.test.notSet')} value={overrides[`${step.id}.response`] ?? ''} oninput={(event) => onOverride(`${step.id}.response`, event.currentTarget.value)} />
              </Field>
            {/if}
            {#if step.node?.type === 'cond.var'}
              <Field label={t('flows.test.stored', { name: String(step.node.config.name || t('flows.test.notSet')) })} for="{uid}-{step.key}-stored">
                <Input id="{uid}-{step.key}-stored" fill placeholder={t('flows.test.notSet')} value={stored(step)} oninput={(event) => step.node && onScenario(setStoredValue(scenario, step.node, event.currentTarget.value))} />
              </Field>
            {/if}
            {#if step.kind === 'check' && !pretend}
              <div class="repair" class:invisible={!step.repair} inert={!step.repair}>
                <Button variant="ghost" size="sm" onclick={() => step.repair && onScenario(step.repair)}><span data-fit>{t('flows.test.makePass')}</span></Button>
              </div>
            {/if}
            {#if row.more}<span class="context">{t('flows.test.moreRows', { count: row.more })}</span>{/if}
          </article>
        {/each}
      </div>
    </li>
  {/each}
</ol>

<style>
  .timeline { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 20px; }
  .group { display: flex; align-items: flex-start; gap: 10px; }
  .time { flex: 0 0 36px; padding-top: 8px; font-size: 12px; font-variant-numeric: tabular-nums; color: var(--bb-muted-light); }
  .rows { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 12px; }
  .row { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .select { display: flex; flex-direction: column; gap: 3px; width: 100%; padding: 6px 0; border: 0; background: transparent; color: inherit; text-align: start; cursor: pointer; font: inherit; }
  .select:disabled { cursor: default; }
  .select:hover { color: var(--bb-green-glow); }
  .select:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 3px; border-radius: var(--bb-radius-xs); }
  .label { width: 100%; height: 22px; font-size: 15px; font-weight: 600; }
  .detail, .result, .missing { font-size: 14px; line-height: 1.45; overflow-wrap: anywhere; }
  .detail { width: 100%; height: 42px; }
  .context { font-size: 12px; line-height: 1.4; color: var(--bb-muted-light); }
  .result { font-weight: 600; }
  .muted { opacity: 0.6; }
  .bubble { padding: 10px 12px; border-radius: var(--bb-radius-sm); background: var(--bb-bg-2); }
  .chat-line { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 14px; line-height: 1.5; }
  .chat-text { overflow-wrap: anywhere; min-width: 0; }
  .missing { color: var(--bb-tan); }
  .pretend { display: flex; flex-direction: column; gap: 4px; }
  .repair { min-height: 32px; }
  .invisible { visibility: hidden; opacity: 0; }
</style>
