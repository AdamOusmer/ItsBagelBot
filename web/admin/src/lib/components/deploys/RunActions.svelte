<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { applyAction, enhance, type SubmitFunction } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRun } from '#lib/deploys/types';
  import { approvalStage, cancellable, failedStage, offered } from './view';

  let { run, onrun }: { run: DeployRun; onrun: (run: DeployRun) => void } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  type Verb = {
    id: string;
    action: '?/resume' | '?/cancel' | '?/approve';
    label: string;
    fields: Record<string, string>;
    variant: 'primary' | 'secondary' | 'ghost';
    available: boolean;
  };

  const acts = $derived(offered(run));
  const approval = $derived(approvalStage(run));
  const stoppedAt = $derived(failedStage(run)?.id ?? '');
  const rollbackTo = $derived(run.outputs.live_version ?? '');
  const canCancel = $derived(cancellable(run, acts));
  const canRollback = $derived(acts.has('rollback') && rollbackTo !== '');

  const verbs = $derived<Verb[]>([
    {
      id: 'approve',
      action: '?/approve',
      label: t('admin.deploys.act.approve'),
      fields: { run_id: run.id, stage: approval?.id ?? '' },
      variant: 'primary',
      available: approval !== undefined
    },
    {
      id: 'resume',
      action: '?/resume',
      label: t('admin.deploys.act.resume'),
      fields: { run_id: run.id },
      variant: 'primary',
      available: acts.has('resume')
    },
    {
      id: 'retry',
      action: '?/resume',
      label: t('admin.deploys.act.retry'),
      fields: { run_id: run.id, stage: stoppedAt, rerun: 'true' },
      variant: 'secondary',
      available: acts.has('rerun_failed_jobs')
    },
    {
      id: 'cancel',
      action: '?/cancel',
      label: t('admin.deploys.act.cancel'),
      fields: { run_id: run.id },
      variant: 'ghost',
      available: canCancel
    }
  ]);

  let busy = $state(false);
  let rollbackOpen = $state(false);
  let rollbackForm = $state<HTMLFormElement | null>(null);

  const submitVerb: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload<{ run?: DeployRun; error?: string }>(result);
      if (p?.run) return onrun(p.run);
      failed(p, t('admin.deploys.actionFailed'));
    };
  };

  const submitRollback: SubmitFunction = () => {
    busy = true;
    rollbackOpen = false;
    return async ({ result }) => {
      busy = false;
      if (result.type !== 'failure') return applyAction(result);
      failed(actionPayload(result), t('admin.deploys.startFailed'));
    };
  };
</script>

<div class="run-verbs">
  {#each verbs as verb (verb.id)}
    <form
      class="verb"
      class:unavailable={!verb.available}
      inert={!verb.available}
      method="POST"
      action={verb.action}
      use:enhance={submitVerb}
    >
      {#each Object.entries(verb.fields) as [name, value] (name)}
        <input type="hidden" {name} {value} />
      {/each}
      <Button type="submit" variant={verb.variant} block disabled={busy || !verb.available}>
        <bb-fit><span data-fit>{verb.label}</span></bb-fit>
      </Button>
    </form>
  {/each}
  <form
    class="verb rollback"
    class:unavailable={!canRollback}
    inert={!canRollback}
    method="POST"
    action="/deploys?/start"
    use:enhance={submitRollback}
    bind:this={rollbackForm}
  >
    <input type="hidden" name="kind" value="rollback" />
    <input type="hidden" name="rollback_to" value={rollbackTo} />
    <Button block disabled={busy || !canRollback} onclick={() => (rollbackOpen = true)} tone="danger">
      <bb-fit><span data-fit>{t('admin.deploys.act.rollback', { version: rollbackTo })}</span></bb-fit>
    </Button>
  </form>
</div>

<ConfirmDialog
  open={canRollback && rollbackOpen}
  title={t('admin.deploys.rollbackTitle')}
  body={t('admin.deploys.rollbackBody', { version: rollbackTo })}
  confirmLabel={t('admin.deploys.act.rollback', { version: rollbackTo })}
  cancelLabel={t('admin.deploys.confirmCancel')}
  busyLabel={t('admin.deploys.shipBusy')}
  {busy}
  onConfirm={() => { if (canRollback) rollbackForm?.requestSubmit(); }}
  onCancel={() => (rollbackOpen = false)}
  tone="danger"
/>

<style>
  .run-verbs {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-inline-size: 0;
    block-size: calc(var(--btn-min-h) + var(--bb-space-2));
    padding-inline: var(--bb-space-1);
    overflow: auto hidden;
    scrollbar-width: thin;
  }
  .verb {
    flex: 0 1 7rem;
    min-inline-size: min(5.25rem, 100%);
    transition: opacity var(--bb-dur-base) ease;
  }
  .rollback {
    flex-basis: 11rem;
    min-inline-size: min(8rem, 100%);
  }
  .verb.unavailable {
    position: absolute;
    overflow: hidden;
    inset-inline-start: 0;
    inline-size: 0;
    min-inline-size: 0;
    opacity: 0;
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .verb {
      transition: none;
    }
  }
</style>
