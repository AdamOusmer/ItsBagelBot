<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // Start a run: pick the kind, check what it will do, confirm, submit ?/start.
  //
  // Every field is on screen for every kind and the ones a kind does not use
  // are disabled, not removed. Switching kind is the most common click here,
  // and a panel that grows and shrinks under it moves the Ship button away
  // from the pointer. Disabled inputs are also left out of the form data, so
  // the same markup sends only what the picked kind reads.
  //
  // The load's plan answers for a release. Any other kind asks ?/plan again,
  // because the services a bump rolls are the ones whose pin would change,
  // which only the deployer knows.
  //
  // The service chips follow the same rule. Each kind's plan lists a
  // different set (a release every image, a bump only the changed ones), so
  // the row renders every service any plan has named and dims the ones the
  // picked kind will not roll: the chip count never drops, and the Ship
  // button below it stays where it was on a single column layout.
  import { untrack } from 'svelte';
  import { applyAction, enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RUN_KINDS } from '$lib/deploys/types';
  import type { DeployPlan, DeployRun, RunKind } from '$lib/deploys/types';
  import PRPicker from './PRPicker.svelte';
  import CommitList from './CommitList.svelte';
  import ChangelogDraft from './ChangelogDraft.svelte';
  import { CONFIRM_KEY, KIND_HINT_KEY, KIND_KEY, shortSha, tickable } from './view';
  import {
    blocker,
    changelogJSON,
    commitLines,
    confirmParams,
    defaultVersion,
    fetchPlan,
    shipKey,
    countKey,
    shipVersion,
    usesFor,
    type ChangelogDraft as Draft,
    type ShipState
  } from './ship';

  let { plan, active }: { plan: DeployPlan | null; active: DeployRun | null } = $props();

  const { t } = getI18n();

  // SegmentedControl renders its option strings as the button text, so the
  // options are the translated labels and the kind is read back by index.
  const kindOptions = RUN_KINDS.map((k) => t(KIND_KEY[k]));
  let kindLabel = $state(kindOptions[0]);
  const kind = $derived<RunKind>(RUN_KINDS[kindOptions.indexOf(kindLabel)] ?? 'release');
  const uses = $derived(usesFor(kind));

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
  let startError = $state('');
  let seen = $state<string[]>(untrack(() => plan?.services ?? []));
  let form = $state<HTMLFormElement | null>(null);

  // A late answer for a kind the operator already switched away from must
  // not overwrite the plan for the one now picked.
  let planGen = 0;
  let planned: RunKind = 'release';
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

  $effect(() => {
    const k = kind;
    if (k === planned) return;
    planned = k;
    version = defaultVersion(k, untrack(() => shown));
    plannedTarget = untrack(() => rollbackTo);
    void replan(k, k === 'rollback' ? plannedTarget : '');
  });

  // A rollback pins only the images its target release built, so picking a
  // target re-asks the plan for the services that will actually change.
  $effect(() => {
    const target = rollbackTo;
    if (untrack(() => kind) !== 'rollback' || target === plannedTarget) return;
    plannedTarget = target;
    void replan('rollback', target);
  });

  // A refused start (409 run active, 400 invalid) answers the fields as
  // they were. Once the kind, version or rollback target changes, that
  // answer no longer describes the form and would sit over the live reason.
  $effect(() => {
    void [kind, version, rollbackTo];
    startError = '';
  });

  const prs = $derived(shown?.prs ?? []);
  const services = $derived(shown?.services ?? []);
  const releases = $derived(shown?.releases ?? []);
  const pickedCount = $derived(prs.filter((p) => picked.includes(p.number) && tickable(p)).length);

  const ship = $derived<ShipState>({
    kind,
    uses,
    active: active !== null,
    version,
    targetSha: shown?.main_sha ?? '',
    rollbackTo,
    titleEn: draft.titleEn,
    replanning,
    planFailed
  });
  const blockKey = $derived(blocker(ship));

  // The live blocker outranks a start error: it is why the button is
  // disabled right now, and the error answered an earlier submit.
  function slotMessage(): string {
    return blockKey ? t(blockKey) : startError;
  }

  function servicesNote(): string {
    if (replanning) return t('admin.deploys.servicesLoading');
    return services.length === 0 ? t('admin.deploys.servicesNone') : '';
  }

  function counted(key: string, n: number): string {
    return t(countKey(n, key), { n: String(n) });
  }

  function confirmText(): string {
    const line = t(CONFIRM_KEY[kind], confirmParams(ship, counted('admin.deploys.servicesCount', services.length)));
    if (!uses.prs || pickedCount === 0) return line;
    return `${counted('admin.deploys.confirmPrs', pickedCount)} ${line}`;
  }

  function confirmShip() {
    confirmed = true;
    form?.requestSubmit();
  }

  // Only the dialog's confirm submits. Enter in a text field would otherwise
  // start a production deploy with no confirmation at all.
  const submitStart: SubmitFunction = ({ cancel }) => {
    if (!confirmed) return cancel();
    confirmed = false;
    busy = true;
    confirmOpen = false;
    startError = '';
    return async ({ result }) => {
      busy = false;
      if (result.type !== 'failure') return applyAction(result);
      startError = (result.data as { error?: string } | undefined)?.error ?? t('admin.deploys.startFailed');
    };
  };
</script>

<Card>
  <CardHead title={t('admin.deploys.shipTitle')}>
    {#snippet action()}
      <SegmentedControl options={kindOptions} bind:value={kindLabel} label={t('admin.deploys.kindLabel')} />
    {/snippet}
  </CardHead>
  <p class="hint">{t(KIND_HINT_KEY[kind])}</p>

  <form method="POST" action="/deploys?/start" use:enhance={submitStart} bind:this={form}>
    <input type="hidden" name="kind" value={kind} />
    <input type="hidden" name="target_sha" value={ship.targetSha} disabled={!uses.target} />
    <input type="hidden" name="changelog" value={changelogJSON(draft)} disabled={!uses.changelog} />

    <div class="fields">
      <Field label={t('admin.deploys.version')} for="ship-version">
        <Input id="ship-version" name="version" mono fill bind:value={version} disabled={!uses.version} />
      </Field>
      <Field label={t('admin.deploys.target')}>
        <span class="sha">{uses.target && ship.targetSha ? shortSha(ship.targetSha) : '-'}</span>
      </Field>
      <Field label={t('admin.deploys.rollbackTo')} for="ship-rollback">
        <Select id="ship-rollback" name="rollback_to" fill bind:value={rollbackTo} disabled={!uses.rollback}>
          <option value="">{t('admin.deploys.rollbackPick')}</option>
          {#each releases as r (r.version)}
            <option value={r.version}>{r.version}</option>
          {/each}
        </Select>
      </Field>
    </div>

    <div class="body">
      <section class="col" aria-labelledby="ship-prs">
        <h3 class="sub" id="ship-prs">{t('admin.deploys.prs')}</h3>
        <p class="note">{uses.prs ? t('admin.deploys.prBlocked') : t('admin.deploys.prsUnused')}</p>
        <PRPicker {prs} bind:picked disabled={!uses.prs} />
        <CommitList commits={shown?.commits ?? []} since={shown?.last_tag ?? '-'} />
        <h3 class="sub">
          {t('admin.deploys.services')}
          <span class="aside">{servicesNote()}</span>
        </h3>
        <div class="chips">
          {#each seen as s (s)}
            {@const rolls = services.includes(s)}
            <span class="chip" class:off={!rolls} aria-hidden={!rolls}>
              <Tag tone={rolls ? 'live' : 'quiet'} mark={rolls ? 'solid' : 'hollow'}>{s}</Tag>
            </span>
          {/each}
        </div>
      </section>
      <section class="col" aria-labelledby="ship-changelog">
        <h3 class="sub" id="ship-changelog">{t('admin.deploys.changelog')}</h3>
        <p class="note">
          {uses.changelog ? t('admin.deploys.changelogHint') : t('admin.deploys.changelogUnused')}
        </p>
        <ChangelogDraft bind:draft disabled={!uses.changelog} />
      </section>
    </div>

    <div class="foot">
      <p class="slot" role="status" aria-live="polite">{slotMessage()}</p>
      <Button disabled={blockKey !== null} loading={busy} onclick={() => (confirmOpen = true)}>
        {t(shipKey(kind), { version: shipVersion(ship) })}
      </Button>
    </div>
  </form>
</Card>

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
  .hint {
    margin: 0 0 16px;
    min-height: 1.5em;
    font-size: 13px;
    color: var(--bb-muted);
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
    margin-bottom: 20px;
  }
  .sha {
    display: flex;
    align-items: center;
    height: 38px;
    font-family: var(--bb-font-mono);
    font-size: 13px;
  }
  .body {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .sub {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin: 8px 0 0;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    font-weight: 500;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .aside {
    text-transform: none;
    letter-spacing: 0;
  }
  .note {
    margin: 0;
    min-height: 1.5em;
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    min-height: 28px;
  }
  .chip.off {
    opacity: 0.45;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    margin-top: 20px;
  }
  .slot {
    flex: 1;
    margin: 0;
    min-height: 1.5em;
    font-size: 13px;
    color: var(--bb-muted);
    text-align: right;
  }
  @media (max-width: 760px) {
    .fields,
    .body {
      grid-template-columns: 1fr;
    }
    .foot {
      flex-direction: column;
      align-items: stretch;
    }
    .slot {
      text-align: left;
    }
  }
</style>
