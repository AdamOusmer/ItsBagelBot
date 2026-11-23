<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { goto, invalidate } from '$app/navigation';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { giveawayNotice } from '#lib/giveaway-workflow';
  import { CAMPAIGN_NAME_MAX, type CampaignCreatedPayload } from '#lib/campaign-workflow';

  let { result }: { result?: unknown } = $props();
  const { t } = getI18n();

  let name = $state('');
  let busy = $state(false);
  const notice = $derived(giveawayNotice(result));

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy) return cancel();
    busy = true;
    return async ({ result, update }) => {
      try {
        await update({ reset: false, refreshAll: false, navigate: false });
        const created = actionPayload<CampaignCreatedPayload>(result);
        if (!created?.action?.ok) return;
        await invalidate('admin:campaigns');
        await goto(`/campaigns/${created.campaign?.id}`);
      } finally {
        busy = false;
      }
    };
  };
</script>

<Card as="section">
  <Stack gap={3}>
    <CardHead title={t('admin.campaigns.newTitle')} headingLevel={2} />
    <form method="POST" action="?/create" use:enhance={submit} aria-busy={busy} style="--field-mb: 0">
      <Stack gap={3}>
        <Field label={t('admin.campaigns.fieldName')} fit>
          <Input name="name" bind:value={name} required maxlength={CAMPAIGN_NAME_MAX} />
        </Field>
        <div class="draft-action">
          <Button type="submit" block {busy}>
            <bb-fit><span data-fit>{t('admin.campaigns.create')}</span></bb-fit>
          </Button>
        </div>
      </Stack>
    </form>
    <div class="action-feedback" aria-live="polite">
      {#if notice}
        <AlertBanner tone={notice.failed ? 'danger' : 'success'} role="note" flush>{notice.text}</AlertBanner>
      {/if}
    </div>
  </Stack>
</Card>

<style>
  .draft-action {
    max-inline-size: 15rem;
  }

  .action-feedback {
    min-block-size: 3rem;
  }
</style>
