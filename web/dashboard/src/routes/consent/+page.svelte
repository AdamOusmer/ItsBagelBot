<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import SetupStep from '@bagel/ui/svelte/SetupStep.svelte';
  import ConsentForm from '#lib/components/ConsentForm.svelte';

  let { data, form } = $props();
  const i18n = getI18n();
  const { t } = i18n;
  const title = $derived(t('onboarding.consentTitle'));
</script>

<svelte:head>
  <title>{title} · ItsBagelBot</title>
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<SetupStage label={title} orbs={false}>
  {#snippet start()}<Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />{/snippet}
  <SetupStep title={title} kicker="" instant>
    <ConsentForm locale={i18n.locale} requestId={data.requestId} maskedEmail={data.maskedEmail} serverError={form?.error} />
  </SetupStep>
</SetupStage>
