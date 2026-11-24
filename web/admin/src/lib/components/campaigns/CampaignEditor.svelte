<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { giveawayNotice } from '#lib/giveaway-workflow';
  import {
    CAMPAIGN_BODY_MAX,
    CAMPAIGN_LOCALES,
    CAMPAIGN_NAME_MAX,
    CAMPAIGN_STATUS,
    CAMPAIGN_SUBJECT_MAX,
    contentPayload,
    draftContent,
    type CampaignLocale
  } from '#lib/campaign-workflow';
  import type { CampaignAudience, CampaignWire } from '#lib/server/campaigns';
  import CampaignControls from './CampaignControls.svelte';
  import CampaignStats from './CampaignStats.svelte';

  let { campaign, audience, canApprove, result }: {
    campaign: CampaignWire;
    audience: Promise<CampaignAudience | null>;
    canApprove: boolean;
    result?: unknown;
  } = $props();

  const { t } = getI18n();
  const localeOptions = CAMPAIGN_LOCALES.map((value) => ({ value, label: value.toUpperCase() }));

  // svelte-ignore state_referenced_locally
  let name = $state(campaign.name);
  // svelte-ignore state_referenced_locally
  let content = $state(draftContent(campaign.content));
  let locale = $state<CampaignLocale>('en');
  let busy = $state<string>();

  const editable = $derived(campaign.status === 'draft');
  const status = $derived(CAMPAIGN_STATUS[campaign.status]);
  const serialized = $derived(JSON.stringify(contentPayload(content)));
  const dirty = $derived(name !== campaign.name || serialized !== JSON.stringify(contentPayload(draftContent(campaign.content))));
  const notice = $derived(giveawayNotice(result));

  const submit: SubmitFunction = ({ action, cancel }) => {
    if (busy) return cancel();
    busy = action.search.slice(2);
    return async ({ result, update }) => {
      try {
        await update({ reset: false, refreshAll: false, navigate: false });
        if (actionPayload<{ action?: { ok?: boolean } }>(result)?.action?.ok) await invalidate('admin:campaign');
      } finally {
        busy = undefined;
      }
    };
  };
</script>

{#snippet back()}
  <Cluster gap={3}>
    <TextLink variant="quiet" href="/campaigns" icon="arrowLeft" touch>{t('admin.campaigns.back')}</TextLink>
    <span class="campaign-status">
      <Badge shape="status" tone={status.tone}><bb-fit><span data-fit>{t(status.label)}</span></bb-fit></Badge>
    </span>
  </Cluster>
{/snippet}

<PageHead>
  <em>{campaign.name}</em>
</PageHead>

<PageToolbar leading={back} />

<Stack gap={4}>
  <div class="operation-notice" aria-live="polite">
    {#if notice}
      <AlertBanner tone={notice.failed ? 'danger' : 'success'} role="note" flush>{notice.text}</AlertBanner>
    {/if}
  </div>

  <Grid min="25rem" gap={4}>
    <Card as="section">
      <Stack gap={3}>
        <CardHead title={t('admin.campaigns.contentTitle')} headingLevel={2} />
        <form method="POST" action="?/save" use:enhance={submit} aria-busy={busy === 'save'} style="--field-mb: 0">
          <input type="hidden" name="content" value={serialized} />
          <Stack gap={3}>
            <Field label={t('admin.campaigns.fieldName')} fit>
              <Input name="name" bind:value={name} required maxlength={CAMPAIGN_NAME_MAX} readonly={!editable} />
            </Field>
            <SegmentedControl options={localeOptions} bind:value={locale} label={t('admin.campaigns.language')} />
            <Field label={t('admin.campaigns.fieldSubject')} fit>
              <Input bind:value={content[locale].subject} maxlength={CAMPAIGN_SUBJECT_MAX} readonly={!editable} />
            </Field>
            <Field label={t('admin.campaigns.fieldHtml')} fit>
              <Textarea bind:value={content[locale].html} rows={12} maxlength={CAMPAIGN_BODY_MAX} mono readonly={!editable} />
            </Field>
            <Field label={t('admin.campaigns.fieldText')} fit>
              <Textarea bind:value={content[locale].text} rows={6} maxlength={CAMPAIGN_BODY_MAX} mono readonly={!editable} />
            </Field>
            <div class="save-action">
              <Button type="submit" block busy={busy === 'save'} disabled={!editable || !dirty || Boolean(busy)}>
                <bb-fit><span data-fit>{t('admin.campaigns.save')}</span></bb-fit>
              </Button>
            </div>
          </Stack>
        </form>
      </Stack>
    </Card>

    <Card as="section">
      <Stack gap={3}>
        <CardHead title={t('admin.campaigns.previewTitle')} headingLevel={2} />
        <iframe class="preview" title={t('admin.campaigns.previewFrame')} sandbox="" referrerpolicy="no-referrer" srcdoc={content[locale].html}></iframe>
      </Stack>
    </Card>
  </Grid>

  <CampaignControls {campaign} {dirty} {canApprove} {busy} {submit} />
  <CampaignStats {campaign} {audience} />
</Stack>

<style>
  .campaign-status {
    display: inline-grid;
    inline-size: 6.5rem;
  }

  .operation-notice {
    min-block-size: 3rem;
  }

  .save-action {
    max-inline-size: 15rem;
  }

  .preview {
    inline-size: 100%;
    block-size: 30rem;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    color-scheme: light;
  }
</style>
