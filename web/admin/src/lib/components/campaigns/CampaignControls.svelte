<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { contentComplete } from '#lib/campaign-workflow';
  import type { CampaignWire } from '#lib/server/campaigns';

  let { campaign, dirty, canApprove, busy, submit }: {
    campaign: CampaignWire;
    dirty: boolean;
    canApprove: boolean;
    busy?: string;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  let confirming = $state(false);
  let approveForm = $state<HTMLFormElement>();

  const draft = $derived(campaign.status === 'draft');
  const englishReady = $derived(contentComplete(campaign.content?.en));
  const running = $derived(campaign.status === 'approved' || campaign.status === 'sending');
  const canSend = $derived(draft && canApprove && englishReady && !dirty);
  const hint = $derived.by(() => {
    if (!draft) return 'admin.campaigns.readOnly';
    if (dirty) return 'admin.campaigns.unsaved';
    if (!canApprove) return 'admin.campaigns.approveOwnerOnly';
    if (!englishReady) return 'admin.campaigns.approveNeedsEnglish';
    return 'admin.campaigns.testNote';
  });

  function approve() {
    confirming = false;
    approveForm?.requestSubmit();
  }
</script>

<Card as="section">
  <Stack gap={3}>
    <div class="controls">
      <form method="POST" action="?/test" use:enhance={submit}>
        <Button type="submit" variant="secondary" block busy={busy === 'test'} disabled={dirty || Boolean(busy)}>
          <bb-fit><span data-fit>{t('admin.campaigns.sendTest')}</span></bb-fit>
        </Button>
      </form>
      <form method="POST" action="?/approve" bind:this={approveForm} use:enhance={submit}>
        <input type="hidden" name="revision" value={campaign.revision} />
        <Button type="button" block busy={busy === 'approve'} disabled={!canSend || Boolean(busy)} onclick={() => (confirming = true)}>
          <bb-fit><span data-fit>{t('admin.campaigns.approve')}</span></bb-fit>
        </Button>
      </form>
      <form method="POST" action="?/pause" use:enhance={submit}>
        <Button type="submit" variant="secondary" block busy={busy === 'pause'} disabled={!running || Boolean(busy)}>
          <bb-fit><span data-fit>{t('admin.campaigns.pause')}</span></bb-fit>
        </Button>
      </form>
      <form method="POST" action="?/resume" use:enhance={submit}>
        <Button type="submit" variant="secondary" block busy={busy === 'resume'} disabled={campaign.status !== 'paused' || Boolean(busy)}>
          <bb-fit><span data-fit>{t('admin.campaigns.resume')}</span></bb-fit>
        </Button>
      </form>
    </div>
    <div class="hint">
      <Text size="sm" tone="muted">{t(hint)}</Text>
    </div>
  </Stack>
</Card>

<ConfirmDialog
  bind:open={confirming}
  title={t('admin.campaigns.approveTitle')}
  body={t('admin.campaigns.approveBody')}
  confirmLabel={t('admin.campaigns.approve')}
  onConfirm={approve}
/>

<style>
  .controls {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
    gap: var(--bb-space-2);
  }

  .hint {
    min-block-size: 2.5rem;
  }
</style>
