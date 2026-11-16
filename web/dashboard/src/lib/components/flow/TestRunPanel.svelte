<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import type { RehearsalOverrides, RehearsalScenario } from '#lib/flow/rehearse';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Icon from './FlowIcon.svelte';
  import type { RehearsalView } from '#lib/flow/rehearse';
  import ScenarioInputs from './ScenarioInputs.svelte';
  import RehearsalTimeline from './RehearsalTimeline.svelte';

  let { view, overrides, onScenario, onOverride, onSelect, onClose }: {
    view: RehearsalView;
    overrides: RehearsalOverrides;
    onScenario: (patch: Partial<RehearsalScenario>) => void;
    onOverride: (key: string, value: string) => void;
    onSelect: (id: string, fieldKey?: string) => void;
    onClose: () => void;
  } = $props();
  const { t } = getI18n();
  let rejectedStart = $state(false);
  function makeStart() {
    if (!view.startPatch) return;
    rejectedStart = !view.verifyRepair?.(view.startPatch);
    if (!rejectedStart) onScenario(view.startPatch);
  }
  const stoppedStep = $derived(view.steps.find((step) => step.id === view.stoppedAt)?.label ?? '');
  const headline = $derived({
    not_started: t('flows.test.wouldNotStart', { reason: view.blocked ?? '' }),
    started: t('flows.test.started'),
    stopped: t('flows.test.stoppedAt', { step: stoppedStep })
  }[view.state]);
</script>

<aside class="panel" aria-label={t('flows.board.testRun')}>
  <header class="head">
    <h2 class="name"><bb-fit><span data-fit>{t('flows.board.testRun')}</span></bb-fit></h2>
    <IconButton label={t('flows.board.closeTestRun')} size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
  </header>
  <div class="scroll">
    <p class="summary" class:invisible={Boolean(view.issue)} aria-live="polite"><bb-fit mode="block"><span data-fit>{view.summary}</span></bb-fit></p>
    <p class="note"><bb-fit mode="block"><span data-fit>{t('flows.test.pretend')}</span></bb-fit></p>
    <ScenarioInputs fields={view.fields} scenario={view.scenario} onChange={onScenario} />
    {#if view.issue}
      <div class="notice" role="status">
        <p class="sentence"><bb-fit mode="block"><span data-fit>{view.issue.text}</span></bb-fit></p>
        {#if view.issue.node}<Button variant="ghost" size="sm" onclick={() => view.issue?.node && onSelect(view.issue.node, view.issue.fieldKey)}><span data-fit>{t('flows.test.fixStep')}</span></Button>{/if}
      </div>
    {/if}
    <div class="notice start" class:invisible={Boolean(view.issue)} inert={Boolean(view.issue)}>
      <p class="sentence" role="status"><bb-fit mode="block"><span data-fit>{headline}</span></bb-fit></p>
      <div class="actions">
        <div class="repair" class:invisible={view.state !== 'not_started' || !view.startPatch} inert={view.state !== 'not_started' || !view.startPatch}>
          <Button variant="ghost" size="sm" onclick={makeStart}><span data-fit>{t('flows.test.makeStart')}</span></Button>
        </div>
        <div class="repair" class:invisible={view.state !== 'stopped'} inert={view.state !== 'stopped'}>
          <Button variant="ghost" size="sm" onclick={() => view.stoppedAt && onSelect(view.stoppedAt)}><span data-fit>{t('flows.test.showStep')}</span></Button>
        </div>
      </div>
      <span class="sentence" class:invisible={!rejectedStart} role="status">{rejectedStart ? t('flows.test.repairBlocked') : ''}</span>
    </div>
    <RehearsalTimeline steps={view.steps} scenario={view.scenario} verifyRepair={view.verifyRepair} {overrides} {onScenario} {onOverride} {onSelect} />
  </div>
</aside>

<style>
  .panel { display: flex; flex-direction: column; height: 100%; min-height: 0; overflow: hidden; border-radius: var(--bb-radius-md); background: var(--bb-bg-1); box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65); }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex: none; padding: 12px 12px 8px 16px; }
  .name { flex: 1; height: 24px; margin: 0; min-width: 0; font-size: 17px; font-weight: 600; color: var(--bb-white); }
  .scroll { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; padding: 8px 16px 20px; }
  .summary { flex: none; margin: 0; height: 48px; font-size: 15px; line-height: 1.5; overflow-wrap: anywhere; }
  .note { flex: none; height: 40px; margin: 0; font-size: 13px; line-height: 20px; color: var(--bb-muted-light); }
  .notice { flex: none; min-height: 64px; font-size: 14px; line-height: 1.4; }
  .sentence { height: 42px; margin: 0 0 6px; }
  .actions { display: grid; }
  .repair { min-height: 32px; grid-area: 1 / 1; }
  .invisible { visibility: hidden; opacity: 0; }
</style>
