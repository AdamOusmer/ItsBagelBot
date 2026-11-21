<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
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

  let { present, botLink }: { present: boolean; botLink: string } = $props();

  const { t } = getI18n();

  let copied = $state(false);

  const copy = () => copyFlash(botLink, (on) => (copied = on));
</script>

<Card as="section">
  <CardHead title={t('admin.overview.botTitle')} headingLevel={2} />

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
        <span class="copy-status" aria-live="polite">
          <span class="copy-label" class:visible={!copied} aria-hidden={copied}>{t('common.copy')}</span>
          <span class="copy-label" class:visible={copied} aria-hidden={!copied}>{t('common.copied')}</span>
        </span>
      </Button>
    </Cluster>
  </div>
</Card>

<style>
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
  }
  .cta {
    white-space: nowrap;
  }

  .link {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    margin-top: var(--bb-space-3);
  }

  .copy-status {
    display: grid;
  }

  .copy-label {
    grid-area: 1 / 1;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .copy-label.visible {
    opacity: 1;
  }

  @container (max-width: 28rem) {
    .row {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .cta {
      grid-column: 2;
      justify-self: start;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .copy-label {
      transition: none;
    }
  }
</style>
