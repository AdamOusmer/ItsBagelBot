<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { applyAction, enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployPlan, RunKind } from '$lib/deploys/types';
  import DeployScreen from './DeployScreen.svelte';
  import StepChecklist, { type ChecklistItem } from './StepChecklist.svelte';
  import StageScene from './StageScene.svelte';
  import Mascot from './Mascot.svelte';
  import KindPicker from './KindPicker.svelte';
  import ReleasePicker from './ReleasePicker.svelte';
  import PRPicker from './PRPicker.svelte';
  import CommitList from './CommitList.svelte';
  import ChangelogDraft from './ChangelogDraft.svelte';
  import ShipReview from './ShipReview.svelte';
  import { CONFIRM_KEY, KIND_KEY, shortSha, tickable } from './view';
  import {
    STEP_BODY_KEY,
    STEP_RAIL_KEY,
    STEP_TITLE_KEY,
    changelogJSON,
    commitLines,
    confirmParams,
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

  let { plan, planError }: { plan: DeployPlan | null; planError: string | null } = $props();

  const { t } = getI18n();

  let kind = $state<RunKind>('release');
  const uses = $derived(usesFor(kind));
  const steps = $derived(stepsFor(uses));

  let step = $state(0);
  let furthest = $state(0);
  let dir = $state(1);

  let shown = $state<DeployPlan | null>(untrack(() => plan));
  let replanning = $state(false);
  let planFailed = $state(false);
  let rollbackTo = $state('');
  let version = $state(untrack(() => defaultVersion('release', plan)));
  let picked = $state<number[]>([]);
  let draft = $state<Draft>({
    titleEn: '',
    titleFr: '',
    highlightsEn: untrack(() => commitLines(plan?.commits)),
    highlightsFr: ''
  });
  let confirmOpen = $state(false);
  let confirmed = false;
  let busy = $state(false);
  let leaving = $state(false);
  let startError = $state('');
  let seen = $state<string[]>(untrack(() => plan?.services ?? []));
  let form = $state<HTMLFormElement | null>(null);

  let planGen = 0;
  let plannedTarget = '';

  async function replan(k: RunKind, target = '') {
    const gen = ++planGen;
    replanning = true;
    const next = await fetchPlan(k, target);
    if (gen !== planGen) return;
    replanning = false;
    planFailed = next === null;
    if (!next) return;
    shown = next;
    seen = [...seen, ...(next.services ?? []).filter((s) => !seen.includes(s))];
  }

  function pickKind(k: RunKind) {
    if (k === kind && !planFailed) return;
    kind = k;
    furthest = 0;
    version = defaultVersion(k, shown);
    plannedTarget = rollbackTo;
    react('excited');
    void replan(k, k === 'rollback' ? plannedTarget : '');
  }

  $effect(() => {
    const target = rollbackTo;
    if (untrack(() => kind) !== 'rollback' || target === plannedTarget) return;
    plannedTarget = target;
    void replan('rollback', target);
  });

  $effect(() => {
    void [kind, version, rollbackTo, changelogJSON(draft), picked.length];
    startError = '';
  });

  const prs = $derived(shown?.prs ?? []);
  const services = $derived(shown?.services ?? []);
  const releases = $derived(shown?.releases ?? []);
  const shippedPrs = $derived(uses.prs ? prs.filter((p) => picked.includes(p.number) && tickable(p)) : []);

  const ship = $derived<ShipState>({
    kind,
    uses,
    version,
    targetSha: shown?.main_sha ?? '',
    rollbackTo,
    titleEn: draft.titleEn,
    replanning,
    planFailed
  });

  const current = $derived(steps[Math.min(step, steps.length - 1)]);
  const last = $derived(step === steps.length - 1);
  const maxStep = $derived(reachable(ship, steps, furthest));
  const blockKey = $derived(stepBlocker(ship, current));
  const progress = $derived(step / Math.max(steps.length - 1, 1));
  const stepOf = $derived(t('admin.deploys.flow.stepOf', { n: String(step + 1), total: String(steps.length) }));

  function counted(key: string, n: number): string {
    return t(countKey(n, key), { n: String(n) });
  }

  const SUMMARY: Record<ShipStep, () => string> = {
    kind: () => t(KIND_KEY[kind]),
    build: () => [version.trim(), shortSha(ship.targetSha)].filter(Boolean).join(' · '),
    rollback: () => rollbackTo || t('admin.deploys.rollbackPick'),
    prs: () => (shippedPrs.length === 0 ? t('admin.deploys.flow.reviewPrsNone') : counted('admin.deploys.flow.picked', shippedPrs.length)),
    changelog: () => draft.titleEn.trim() || '-',
    review: () => t(shipKey(kind), { version: shipVersion(ship) })
  };

  const checklist = $derived<ChecklistItem[]>(
    steps.map((s, i) => ({
      id: s,
      label: t(STEP_RAIL_KEY[s]),
      meta: i > maxStep ? t('admin.deploys.flow.pending') : SUMMARY[s](),
      state: i < step ? 'succeeded' : i === step ? 'running' : 'pending',
      value: i < step ? 1 : 0,
      disabled: i > maxStep
    }))
  );

  const FACE: Record<ShipStep, string> = {
    kind: 'curious',
    build: 'attentive',
    rollback: 'attentive',
    prs: 'curious',
    changelog: 'happy',
    review: 'excited'
  };

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';
  let sequence = $state<Sequence>('entrance');
  let sequenceKey = $state(0);
  let reaction = $state<string | null>(null);
  let reactionTimer: ReturnType<typeof setTimeout> | null = null;
  const expression = $derived(reaction ?? (blockKey && last ? 'attentive' : FACE[current]));
  function react(face: string, ms = 1400) {
    reaction = face;
    if (reactionTimer) clearTimeout(reactionTimer);
    reactionTimer = setTimeout(() => (reaction = null), ms);
  }
  function play(seq: Sequence) {
    sequence = seq;
    sequenceKey += 1;
  }

  let heading = $state<HTMLHeadingElement | null>(null);
  let settled = false;
  $effect(() => {
    void step;
    if (settled) heading?.focus({ preventScroll: true });
    settled = true;
  });

  function go(i: number) {
    const target = Math.max(0, Math.min(i, maxStep));
    if (target === step || leaving) return;
    dir = target > step ? 1 : -1;
    step = target;
    furthest = Math.max(furthest, target);
    play('entrance');
  }
  const goNext = () => go(step + 1);
  const goBack = () => go(step - 1);
  const selectStep = (id: string) => go(steps.indexOf(id as ShipStep));

  const EDITABLE = 'input, textarea, select, [contenteditable]';
  function onKeydown(e: KeyboardEvent) {
    if (confirmOpen || leaving) return;
    if (e.target instanceof Element && e.target.closest(EDITABLE)) return;
    if (e.key === 'ArrowRight' && !last && !blockKey) {
      e.preventDefault();
      goNext();
    } else if (e.key === 'ArrowLeft' && step > 0) {
      e.preventDefault();
      goBack();
    }
  }

  function slotMessage(): string {
    if (blockKey) return t(blockKey);
    if (last) return startError;
    if (current !== 'prs') return '';
    if (shippedPrs.length === 0) return t('admin.deploys.flow.pickedNone');
    return counted('admin.deploys.flow.picked', shippedPrs.length);
  }

  function confirmText(): string {
    const line = t(CONFIRM_KEY[kind], confirmParams(ship, counted('admin.deploys.servicesCount', services.length)));
    if (shippedPrs.length === 0) return line;
    return `${counted('admin.deploys.confirmPrs', shippedPrs.length)} ${line}`;
  }

  const EXIT_MS = 640;
  function confirmShip() {
    confirmed = true;
    confirmOpen = false;
    leaving = true;
    react('proud', 4000);
    play('burst');
    setTimeout(() => form?.requestSubmit(), prefersReducedMotion() ? 0 : EXIT_MS);
  }

  // Only the dialog's confirm submits: Enter in a field would start a production deploy unconfirmed.
  const submitStart: SubmitFunction = ({ cancel }) => {
    if (!confirmed) return cancel();
    confirmed = false;
    busy = true;
    startError = '';
    return async ({ result }) => {
      busy = false;
      if (result.type !== 'failure') return applyAction(result);
      leaving = false;
      react('surprised', 2400);
      startError = (result.data as { error?: string } | undefined)?.error ?? t('admin.deploys.startFailed');
    };
  };
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
  progressLabel={t('admin.deploys.flow.label')}
  count={stepOf}
  turn={step}
  {leaving}
>
  {#snippet side()}
    <StepChecklist items={checklist} focus={current} label={t('admin.deploys.flow.label')} onselect={selectStep} />
  {/snippet}

  <form method="POST" action="/deploys?/start" use:enhance={submitStart} bind:this={form} hidden>
    <input type="hidden" name="kind" value={kind} />
    <input type="hidden" name="version" value={version} disabled={!uses.version} />
    <input type="hidden" name="target_sha" value={ship.targetSha} disabled={!uses.target} />
    <input type="hidden" name="rollback_to" value={rollbackTo} disabled={!uses.rollback} />
    <input type="hidden" name="changelog" value={changelogJSON(draft)} disabled={!uses.changelog} />
    {#each shippedPrs as pr (pr.number)}
      <input type="hidden" name="pr" value={String(pr.number)} />
    {/each}
  </form>

  {#if planError}
    <AlertBanner>{t('admin.deploys.planError', { error: planError })}</AlertBanner>
  {/if}

  <div class="stage-row" class:wide={current === 'kind'} class:leaving>
    <div class="scenes">
      {#key step}
        <StageScene
          {dir}
          title={t(STEP_TITLE_KEY[current])}
          body={t(STEP_BODY_KEY[current])}
          headingId="ship-step-title"
          center={current === 'kind'}
          bind:heading
        >
          {#snippet kicker()}{stepOf}{/snippet}
          {#snippet children()}
            {#if current === 'kind'}
              <KindPicker {kind} onpick={pickKind} />
            {:else if current === 'build'}
              <div class="build">
                <div class="build-fields">
                  {#if uses.version}
                    <Field label={t('admin.deploys.version')} for="ship-version">
                      <Input id="ship-version" mono fill bind:value={version} />
                    </Field>
                  {/if}
                  <Field label={t('admin.deploys.target')}>
                    <span class="sha">{ship.targetSha ? shortSha(ship.targetSha) : '-'}</span>
                  </Field>
                </div>
                <div class="well"><CommitList commits={shown?.commits ?? []} since={shown?.last_tag ?? '-'} open /></div>
              </div>
            {:else if current === 'rollback'}
              <ReleasePicker {releases} bind:value={rollbackTo} />
            {:else if current === 'prs'}
              <div class="well"><PRPicker {prs} bind:picked /></div>
            {:else if current === 'changelog'}
              <ChangelogDraft bind:draft />
            {:else}
              <ShipReview {ship} pickedCount={shippedPrs.length} {services} {seen} />
            {/if}
          {/snippet}
          {#snippet actions()}
            {#if last}
              <Button
                variant="green"
                solid
                disabled={blockKey !== null || leaving}
                loading={busy}
                onclick={() => (confirmOpen = true)}
              >
                {t(shipKey(kind), { version: shipVersion(ship) })}
              </Button>
            {:else}
              <Button disabled={blockKey !== null} onclick={goNext}>{t('admin.deploys.flow.next')}</Button>
            {/if}
            <button
              type="button"
              class="quiet"
              class:gone={step === 0}
              tabindex={step === 0 ? -1 : undefined}
              aria-hidden={step === 0}
              onclick={goBack}
            >
              {t('admin.deploys.flow.back')}
            </button>
          {/snippet}
        </StageScene>
      {/key}
    </div>

    <div class="aside" class:gone={current === 'kind'}>
      <Mascot {expression} {sequence} {sequenceKey} />
    </div>
  </div>

  <p class="slot" class:center={current === 'kind'} role="status" aria-live="polite">{slotMessage()}</p>
</DeployScreen>

<ConfirmDialog
  open={confirmOpen}
  title={t('admin.deploys.confirmTitle')}
  body={confirmText()}
  confirmLabel={t('admin.deploys.confirmStart')}
  cancelLabel={t('admin.deploys.confirmCancel')}
  busyLabel={t('admin.deploys.shipBusy')}
  {busy}
  onConfirm={confirmShip}
  onCancel={() => (confirmOpen = false)}
/>

<style>
  .stage-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 260px;
    gap: var(--gap);
    align-items: start;
    transition:
      transform 640ms var(--bb-ease-out-expo),
      opacity 520ms var(--bb-ease-out-expo);
  }
  .stage-row.wide {
    grid-template-columns: minmax(0, 1fr);
  }
  .stage-row.leaving {
    transform: translateX(-6vw);
    opacity: 0;
  }
  .scenes {
    display: grid;
    min-width: 0;
  }
  .aside.gone {
    display: none;
  }

  .build {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .build-fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
    max-width: 560px;
  }
  .sha {
    display: flex;
    align-items: center;
    height: 38px;
    font-family: var(--bb-font-mono);
    font-size: 14px;
    color: var(--bb-tan-pale);
  }
  .well {
    padding: 6px 14px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.05), rgba(0, 0, 0, 0.32));
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.07);
    backdrop-filter: blur(10px);
  }

  .quiet {
    padding: 8px 4px;
    border: 0;
    background: none;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: rgba(255, 255, 255, 0.72);
    cursor: pointer;
    transition:
      color var(--bb-dur-base) var(--bb-ease-out-expo),
      transform var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .quiet:hover {
    color: var(--bb-tan-pale);
    transform: translateX(3px);
  }
  .quiet:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: 2px;
    border-radius: var(--bb-radius-xs);
  }
  .quiet.gone {
    visibility: hidden;
  }

  .slot {
    margin: 0;
    min-height: 1.5em;
    font-size: 13.5px;
    color: var(--bb-tan-pale);
  }
  .slot.center {
    text-align: center;
  }


  @media (max-width: 1180px) {
    .stage-row {
      grid-template-columns: minmax(0, 1fr);
    }
    .aside {
      display: none;
    }
  }
  @media (max-width: 560px) {
    .build-fields {
      grid-template-columns: 1fr;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .stage-row,
    .quiet {
      transition: none;
    }
  }
</style>
