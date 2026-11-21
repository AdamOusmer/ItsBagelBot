<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { type SubmitFunction } from '$app/forms';
  import { beforeNavigate, invalidateAll } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { defaultPoweredConfig, poweredPreviewUrl, type PoweredConfig } from '@bagel/kit/overlays';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import { Autosave } from '#lib/autosave.svelte';
  import { poweredFields } from './powered-form';
  import PoweredControls from './PoweredControls.svelte';
  import PoweredLink from './PoweredLink.svelte';
  import PoweredPreview from './PoweredPreview.svelte';
  import type { ActionData, PageData } from './$types';

  let { data, form }: { data: PageData; form: ActionData } = $props();
  const { t } = getI18n();

  const loadedConfig = (): PoweredConfig => structuredClone(data.powered?.config ?? defaultPoweredConfig());

  let config = $state(untrack(loadedConfig));
  let url = $state(untrack(() => data.powered?.url ?? ''));
  let rotating = $state(false);
  let rotateError = $state(untrack(() => form?.rotateError));

  const autosave = new Autosave({
    revision: untrack(() => data.powered?.revision ?? 0),
    onConflict: async () => {
      await invalidateAll();
      config = loadedConfig();
      autosave.reset(data.powered?.revision ?? 0);
    },
    text: { failed: () => t('overlays.saveFailed'), conflict: () => t('overlays.conflict') }
  });

  const previewSrc = $derived(poweredPreviewUrl(config, data.isPremium === true));

  function change(key: keyof PoweredConfig, next: PoweredConfig) {
    config = next;
    autosave.schedule(key, poweredFields(next));
  }

  beforeNavigate(() => void autosave.flush());

  const submitRotate: SubmitFunction = async ({ cancel }) => {
    if (rotating) return cancel();
    rotating = true;
    rotateError = undefined;
    await autosave.flush();
    return async ({ update, result }) => {
      try {
        if (result.type === 'success' && typeof result.data?.url === 'string') {
          url = result.data.url;
          if (typeof result.data.revision === 'number') autosave.revision = result.data.revision;
        }
        if (result.type === 'failure' && typeof result.data?.rotateError === 'string') rotateError = result.data.rotateError;
        await update({ reset: false, invalidateAll: false });
      } finally {
        rotating = false;
      }
    };
  };
</script>

<svelte:head><title>{t('nav.overlays')}</title></svelte:head>

<section class="screen active">
  <PageHead>
    <span class="page-title">
      <bb-fit class="page-title-copy"><span data-fit="">{t('nav.overlays')}</span></bb-fit>
    </span>
  </PageHead>

  {#if data.readOnly}
    <PageNotice tone="info" role="note">{t('overlays.readOnly')}</PageNotice>
  {:else if !data.powered}
    <PageNotice tone="warning">{t('overlays.unavailable')}</PageNotice>
  {:else}
    <Card as="section" flush aria-labelledby="powered-title">
      <div class="section-head">
        <Heading level={6} as="h2" id="powered-title">
          <bb-fit><span data-fit="">{t('overlays.poweredTitle')}</span></bb-fit>
        </Heading>
        <div class="section-copy bb-fit-box">
          <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.poweredIntro')}</bb-fit></Text>
        </div>
      </div>
      <Divider flush />
      <PoweredLink {url} error={rotateError} busy={rotating} submit={submitRotate} />
      <Divider flush />
      <PoweredControls {config} status={autosave.status} onChange={change} />
      <Divider flush />
      <PoweredPreview src={previewSrc} />
    </Card>
  {/if}
</section>

<style>
  .page-title { display: flex; align-items: center; gap: var(--bb-space-3); white-space: nowrap; }
  .page-title-copy { min-inline-size: 0; }
  .section-head { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--bb-space-1); padding: 12px 18px; }
  .section-copy { block-size: 2.5em; min-inline-size: 0; align-content: center; }
</style>
