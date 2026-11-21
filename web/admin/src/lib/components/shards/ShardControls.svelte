<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure, type ShardSnapshot } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { snapshot, minShards, maxShards, changing, stale, beginChange, finishChange }: {
    snapshot: ShardSnapshot | null;
    minShards: number;
    maxShards: number;
    changing: boolean;
    stale: boolean;
    beginChange: () => void;
    finishChange: (snapshot?: ShardSnapshot) => void;
  } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const hintId = $props.id();

  let scaleForm = $state<HTMLFormElement | null>(null);
  let draft = $state<number | '' | undefined>();
  let confirmOpen = $state(false);
  let confirmed = false;
  const autoscale = $derived(snapshot?.autoscale ?? false);
  const base = $derived(snapshot?.desired_count ?? snapshot?.shard_count ?? 1);
  const count = $derived(draft ?? base);
  const numericCount = $derived(count === '' ? Number.NaN : count);
  const inRange = $derived(Number.isInteger(numericCount) && numericCount >= minShards && numericCount <= maxShards);
  const controlsDisabled = $derived(!snapshot || stale || changing);
  const manualDisabled = $derived(controlsDisabled || autoscale);
  const canApply = $derived(!manualDisabled && inRange && count !== base);

  function setDraft(value: string | number | null) {
    draft = typeof value === 'number' ? value : '';
  }

  function step(by: number) {
    const from = count === '' ? base : count;
    draft = Math.min(maxShards, Math.max(minShards, from + by));
  }

  type ShardAction = {
    action?: { ok: boolean; notice: string };
    snapshot?: ShardSnapshot;
    error?: string;
  };

  function submitChange(errorKey: 'admin.shards.scaleFailed' | 'admin.shards.autoscaleFailed'): ReturnType<SubmitFunction> {
    beginChange();
    return async ({ result }) => {
      const response = actionPayload<ShardAction>(result);
      if (result.type !== 'success' || !response?.action?.ok) {
        finishChange();
        failed(response, t(errorKey));
        return;
      }
      finishChange(response.snapshot);
      draft = undefined;
      toast('success', response.action.notice);
    };
  }

  const submitScale: SubmitFunction = ({ cancel, formData }) => {
    if (!canApply) return cancel();
    if (!confirmed && numericCount < base) {
      confirmOpen = true;
      return cancel();
    }
    confirmOpen = false;
    formData.set('count', String(numericCount));
    return submitChange('admin.shards.scaleFailed');
  };

  const submitAutoscale: SubmitFunction = ({ cancel }) => {
    if (controlsDisabled) return cancel();
    return submitChange('admin.shards.autoscaleFailed');
  };

  function confirmScale() {
    confirmOpen = false;
    confirmed = true;
    scaleForm?.requestSubmit();
    confirmed = false;
  }
</script>

<div class="controls">
  <div class="control-row">
    <form method="POST" action="?/autoscale" use:enhance={submitAutoscale} class="autoscale-form">
      <Label as="span"><bb-fit><span data-fit>{t('admin.shards.autoscaleLabel')}</span></bb-fit></Label>
      <input type="hidden" name="enabled" value={String(!autoscale)} />
      <Switch type="submit" checked={autoscale} label={t('admin.shards.autoscaleLabel')}
        describedby={hintId} disabled={!snapshot || stale} busy={changing} />
    </form>

    <form method="POST" action="?/scale" use:enhance={submitScale} bind:this={scaleForm}
      class="scale-form" class:dim={autoscale} aria-label={t('admin.shards.countLabel')}>
      <div class="stepper">
        <IconButton label={t('admin.shards.decrease')} disabled={manualDisabled || numericCount <= minShards}
          onclick={() => step(-1)}><Icon name="minus" /></IconButton>
        <Input mono type="number" name="count" min={minShards} max={maxShards} step={1} required
          bind:value={() => snapshot ? count : '', setDraft} disabled={manualDisabled}
          aria-label={t('admin.shards.countLabel')} />
        <IconButton label={t('admin.shards.increase')} disabled={manualDisabled || numericCount >= maxShards}
          onclick={() => step(1)}><Icon name="plus" /></IconButton>
      </div>
      <Button variant="secondary" type="submit" disabled={!canApply}>
        <span class="apply-label"><bb-fit><span data-fit>{t('admin.shards.apply')}</span></bb-fit></span>
      </Button>
    </form>
  </div>

  <div class="control-hint" class:visible={snapshot !== null} aria-hidden={snapshot === null}>
    <Text size="xs" tone="muted" id={hintId} style="block-size: 100%">
      <bb-fit mode="block"><span data-fit>{t(autoscale ? 'admin.shards.autoscaleOnHint' : 'admin.shards.autoscaleOffHint')}</span></bb-fit>
    </Text>
  </div>
</div>

<ConfirmDialog open={confirmOpen} title={t('admin.shards.confirmScaleDownTitle')}
  body={t('admin.shards.confirmScaleDownBody', { from: String(base), to: String(count) })}
  confirmLabel={t('admin.shards.apply')} cancelLabel={t('common.cancel')}
  busy={changing} onCancel={() => (confirmOpen = false)} onConfirm={confirmScale} tone="danger" />

<style>
  .controls {
    min-inline-size: 0;
  }

  .control-row,
  .scale-form,
  .autoscale-form,
  .stepper {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
  }

  .autoscale-form {
    display: grid;
    grid-template-columns: 6.5rem auto;
  }

  .scale-form {
    transition: opacity var(--bb-dur-fast) ease;
  }

  .scale-form.dim {
    opacity: 0.55;
  }

  .stepper {
    --input-w: 3.7rem;
  }

  .apply-label {
    display: block;
    inline-size: 4.25rem;
  }

  .control-hint {
    block-size: 2.35rem;
    padding-block-start: var(--bb-space-1);
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .control-hint.visible {
    opacity: 1;
  }

  @container (max-width: 460px) {
    .control-row {
      display: grid;
    }

    .autoscale-form {
      justify-content: start;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .scale-form,
    .control-hint {
      transition: none;
    }
  }
</style>
