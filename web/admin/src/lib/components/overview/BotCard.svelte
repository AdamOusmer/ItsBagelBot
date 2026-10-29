<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import MarkBadge from '@bagel/ui/svelte/MarkBadge.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { copyFlash } from '@bagel/ui/lib/clipboard';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';

  let { present }: { present: boolean } = $props();

  const { t } = getI18n();

  let botLink = $state('');
  let copied = $state(false);
  onMount(() => {
    botLink = `${location.origin}/auth/bot/login`;
  });

  const copy = () => copyFlash(botLink, (on) => (copied = on));
</script>

<Card as="section">
  <CardHead title={t('admin.overview.botTitle')} />

  <div class="row">
    <MarkBadge src="/logo.png" size="sm" />
    <Stack gap={1}>
      <Cluster gap={2} nowrap>
        <StatusDot tone={statusTone(present ? 'online' : 'auth_required')} />
        <Eyebrow>{present ? t('admin.overview.botStored') : t('admin.overview.botMissing')}</Eyebrow>
      </Cluster>
      <Text size="sm" tone="muted">
        {present ? t('admin.overview.botStoredMeta') : t('admin.overview.botMissingMeta')}
      </Text>
    </Stack>
    <span class="cta">
      <ButtonLink variant="ghost" href="/auth/bot/login">
        {present ? t('admin.overview.botReauthorize') : t('admin.overview.botAuthorize')}
      </ButtonLink>
    </span>
  </div>

  {#if botLink}
    <div class="link">
      <Text size="sm" tone="muted">{t('admin.overview.botHint')}</Text>
      <Cluster gap={2} nowrap>
        <Input
          fill mono
          type="text"
          readonly
          value={botLink}
          aria-label={t('admin.overview.botLinkLabel')}
        />
        <Button variant="ghost" type="button" onclick={copy}>
          {copied ? t('common.copied') : t('common.copy')}
        </Button>
      </Cluster>
    </div>
  {/if}
</Card>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .cta {
    margin-left: auto;
    white-space: nowrap;
  }

  .link {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 14px;
  }

  @media (max-width: 760px) {
    .row {
      flex-wrap: wrap;
    }
    .cta {
      --btn-w: 100%;
      --btn-justify: center;
      width: 100%;
      margin-left: 0;
    }
  }
</style>
