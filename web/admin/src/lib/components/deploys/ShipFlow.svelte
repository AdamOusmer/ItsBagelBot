<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import StepList, { type StepItem } from '@bagel/ui/svelte/StepList.svelte';
  import Stepper from '@bagel/ui/svelte/Stepper.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { applyStreamed } from '#lib/streamed-load';
  import type { DeployPlan, DeployPlanResult, RunKind } from '#lib/deploys/types';
  import DeployScreen from './DeployScreen.svelte';
  import KindPicker from './KindPicker.svelte';
  import ReleasePicker from './ReleasePicker.svelte';
  import PRPicker from './PRPicker.svelte';
  import ChangelogDraft from './ChangelogDraft.svelte';
  import ShipBuildStep from './ShipBuildStep.svelte';
  import ShipReview from './ShipReview.svelte';
  import ShipStage from './ShipStage.svelte';
  import ShipStart from './ShipStart.svelte';
  import { KIND_KEY, shortSha, tickable } from './view';
  import {
    STEP_RAIL_KEY,
    changelogJSON,
    commitLines,
    countKey,
    defaultVersion,
    fetchPlan,
    reachable,
    shipKey,
    shipVersion,
    stepBlocker,
    stepsFor,
    usesFor,
    type ChangelogDraft as Draft,
    type ShipState,
    type ShipStep
  } from './ship';

  let { initialPlan }: { initialPlan: Promise<DeployPlanResult> } = $props();

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const INTERACTIVE = 'input, textarea, select, button, a[href], summary, [contenteditable], [role="radiogroup"]';

  let kind = $state<RunKind>('release');
  let step = $state(0);
  let furthest = $state(0);

  let shown = $state.raw<DeployPlan | null>(null);
  let initialPending = $state(true);
  let replanning = $state(false);
  let planFailed = $state(false);
  let planError = $state('');
  let planGeneration = 0;
  let plannedTarget = '';

  let rollbackTo = $state('');
  let version = $state('');
  let picked = $state<number[]>([]);
  let draft = $state<Draft>({ titleEn: '', titleFr: '', highlightsEn: '', highlightsFr: '' });
  let seen = $state<string[]>([]);
  let confirmOpen = $state(false);
  let leaving = $state(false);
  let startError = $state('');

  const uses = $derived(usesFor(kind));
  const steps = $derived(stepsFor(uses));
  const prs = $derived(shown?.prs ?? []);
  const services = $derived(shown?.services ?? []);
  const shippedPrs = $derived(uses.prs ? prs.filter((pr) => picked.includes(pr.number) && tickable(pr)) : []);
  const changelog = $derived(changelogJSON(draft));

  const ship = $derived<ShipState>({
    kind,
    uses,
    version,
    targetSha: shown?.main_sha ?? '',
    rollbackTo,
    titleEn: draft.titleEn,
    replanning: initialPending || replanning,
    planFailed
  });

  const current = $derived(steps[step]);
  const last = $derived(step === steps.length - 1);
  const maxStep = $derived(reachable(ship, steps, furthest));
  const blockKey = $derived(stepBlocker(ship, current));
  const progress = $derived(step / Math.max(steps.length - 1, 1));
  const stepOf = $derived(t('admin.deploys.flow.stepOf', { n: number.format(step + 1), total: number.format(steps.length) }));
  const shipLabel = $derived(t(shipKey(kind), { version: shipVersion(ship) }));
  const pickedCount = $derived(shippedPrs.length);
  const pickedLabel = $derived(t(countKey(pickedCount, 'admin.deploys.flow.picked'), { n: number.format(pickedCount) }));
  const prStatus = $derived(pickedCount ? pickedLabel : t('admin.deploys.flow.pickedNone'));
  const feedback = $derived.by(() => {
    if (blockKey) return t(blockKey);
    return current === 'prs' ? prStatus : '';
  });
  const issue = $derived.by(() => {
    if (planError) return t('admin.deploys.planError', { error: planError });
    return planFailed ? t('admin.deploys.planFailed') : startError;
  });
  const railLabel = t('admin.deploys.flow.label');

  const summary: Record<ShipStep, () => string> = {
    kind: () => t(KIND_KEY[kind]),
    build: () => [version.trim(), shortSha(ship.targetSha)].filter(Boolean).join(' · '),
    rollback: () => rollbackTo || t('admin.deploys.rollbackPick'),
    prs: () => pickedCount ? pickedLabel : t('admin.deploys.flow.reviewPrsNone'),
    changelog: () => draft.titleEn.trim() || '-',
    review: () => shipLabel
  };

  function stepState(index: number): StepItem['state'] {
    if (index < step) return 'succeeded';
    return index === step ? 'running' : 'pending';
  }

  const checklist = $derived<StepItem[]>(
    steps.map((id, index) => ({
      id,
      label: t(STEP_RAIL_KEY[id]),
      meta: index > maxStep ? t('admin.deploys.flow.pending') : summary[id](),
      state: stepState(index),
      value: index < step ? 1 : 0,
      disabled: leaving || index > maxStep
    }))
  );

  function acceptPlan(plan: DeployPlan) {
    if (!shown) {
      version = defaultVersion(kind, plan);
      draft.highlightsEn = commitLines(plan.commits);
    }
    shown = plan;
    seen = [...new Set([...seen, ...(plan.services ?? [])])];
  }

  $effect(() => {
    if (!untrack(() => initialPending)) return;
    const planRead = initialPlan.catch(() => ({ plan: null, planError: null }));
    applyStreamed(planRead, ({ plan, planError: error }) => {
      initialPending = false;
      planFailed = !plan;
      planError = error ?? '';
      if (plan) acceptPlan(plan);
    });
  });

  async function replan(nextKind: RunKind, target = '') {
    const generation = ++planGeneration;
    replanning = true;
    planFailed = false;
    planError = '';
    const next = await fetchPlan(nextKind, target);
    if (generation !== planGeneration) return;
    replanning = false;
    planFailed = next === null;
    if (next) acceptPlan(next);
  }

  function pickKind(next: RunKind) {
    if (initialPending) return;
    if (next === kind && !planFailed) return;
    kind = next;
    furthest = 0;
    version = defaultVersion(next, shown);
    plannedTarget = rollbackTo;
    void replan(next, next === 'rollback' ? plannedTarget : '');
  }

  $effect(() => {
    const target = rollbackTo;
    if (untrack(() => kind) !== 'rollback' || target === plannedTarget) return;
    plannedTarget = target;
    void replan('rollback', target);
  });

  $effect(() => {
    void [kind, version, rollbackTo, changelog, picked.length, current];
    startError = '';
  });

  function go(index: number) {
    const target = Math.max(0, Math.min(index, maxStep));
    if (target === step || leaving) return;
    step = target;
    furthest = Math.max(furthest, target);
  }

  function advance() {
    if (blockKey || leaving) return;
    if (last) confirmOpen = true;
    else go(step + 1);
  }

  function permitsStepShortcut(event: KeyboardEvent) {
    const modified = event.altKey || event.ctrlKey || event.metaKey || event.shiftKey;
    const onControl = event.target instanceof Element && event.target.closest(INTERACTIVE);
    return !(event.defaultPrevented || modified || onControl);
  }

  function shortcutStep(key: string) {
    if (key === 'ArrowRight' && !last && !blockKey) return step + 1;
    return key === 'ArrowLeft' ? Math.max(0, step - 1) : step;
  }

  function onKeydown(event: KeyboardEvent) {
    if (confirmOpen || leaving || !permitsStepShortcut(event)) return;
    const target = shortcutStep(event.key);
    if (target === step) return;
    event.preventDefault();
    go(target);
  }
</script>

<svelte:window onkeydown={onKeydown} />

<svelte:head>
  <title>{t('admin.deploys.flow.eyebrow')} · ItsBagelBot Admin</title>
</svelte:head>

<DeployScreen
  eyebrow={t('admin.deploys.flow.eyebrow')}
  name={t(KIND_KEY[kind])}
  em={shipVersion(ship)}
  closeHref="/deploys"
  closeLabel={t('admin.deploys.flow.close')}
  {progress}
  progressLabel={railLabel}
  count={stepOf}
  {leaving}
>
  {#snippet side()}
    <div class="desktop-steps">
      <StepList
        steps={checklist}
        selected={current}
        label={railLabel}
        onSelect={(id) => go(steps.indexOf(id as ShipStep))}
      />
    </div>
    <div class="mobile-steps">
      <Stepper steps={checklist} current={step} maxStep={leaving ? -1 : maxStep} label={railLabel} onSelect={go} />
    </div>
  {/snippet}

  <ShipStage {current} {feedback} {issue} pending={initialPending} {leaving}>
    {#snippet actions()}
      <div class="back" class:unavailable={step === 0}>
        <Button
          variant="ghost"
          --btn-w="5.5rem"
          disabled={step === 0 || leaving}
          aria-hidden={step === 0}
          onclick={() => go(step - 1)}
        >
          <bb-fit><span data-fit>{t('admin.deploys.flow.back')}</span></bb-fit>
        </Button>
      </div>
      <Button
        --btn-w="clamp(9.75rem, 31vw, 13rem)"
        disabled={blockKey !== null || leaving}
        tone={last ? 'success' : 'neutral'}
        busy={leaving}
        onclick={advance}
      >
        <bb-fit><span data-fit>{last ? shipLabel : t('admin.deploys.flow.next')}</span></bb-fit>
      </Button>
    {/snippet}

    {#if current === 'kind'}
      <KindPicker {kind} disabled={initialPending} onpick={pickKind} />
    {:else if current === 'build'}
      <ShipBuildStep plan={shown} {ship} bind:version />
    {:else if current === 'rollback'}
      <ReleasePicker releases={shown?.releases ?? []} bind:value={rollbackTo} />
    {:else if current === 'prs'}
      <PRPicker {prs} bind:picked />
    {:else if current === 'changelog'}
      <ChangelogDraft bind:draft />
    {:else}
      <ShipReview {ship} {pickedCount} {services} {seen} />
    {/if}
  </ShipStage>
</DeployScreen>

<ShipStart
  {ship}
  {changelog}
  prs={shippedPrs}
  serviceCount={services.length}
  bind:open={confirmOpen}
  bind:leaving
  onFailure={(error) => (startError = error)}
/>

<style>
  .mobile-steps {
    display: none;
  }

  .back {
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .back.unavailable {
    opacity: 0;
    pointer-events: none;
  }

  @media (max-width: 960px) {
    .desktop-steps {
      display: none;
    }

    .mobile-steps {
      display: flex;
      justify-content: center;
      padding-block-end: var(--bb-space-2);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .back {
      transition: none;
    }
  }
</style>
