<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NotificationWire } from '$lib/server/services';
  import StatePill from '../StatePill.svelte';
  import { LEVEL_LABEL, LEVEL_TONE, audienceOf } from './notification-compose';

  let {
    notification,
    busy,
    onRetract
  }: {
    notification: NotificationWire;
    busy: boolean;
    onRetract: () => void;
  } = $props();

  const { t } = getI18n();
  const audience = $derived(audienceOf(notification));

  function when(iso?: string): string {
    return iso ? new Date(iso).toLocaleString() : '';
  }
</script>

<div class="detail">
  <Scroller fill padding="18px" smooth>
    <div class="body">
      <Cluster gap={2}>
        <StatePill tone={LEVEL_TONE[notification.level]}>
          {t(LEVEL_LABEL[notification.level])}
        </StatePill>
        <StatePill tone="neutral">{t(audience.key, audience.params)}</StatePill>
      </Cluster>

      <Heading level={5} as="h3" variant="title">{notification.title}</Heading>
      <div class="message"><Text size="sm" tone="muted">{notification.body}</Text></div>

      <FactList>
        <Fact term={t('admin.notifications.factSentBy')}>@{notification.created_by_login}</Fact>
        <Fact term={t('admin.notifications.factSentAt')}>{when(notification.created_at)}</Fact>
        <Fact term={t('admin.notifications.factExpires')}>
          {notification.expires_at
            ? when(notification.expires_at)
            : t('admin.notifications.factNoExpiry')}
        </Fact>
        <Fact term={t('admin.notifications.factRead')}>
          {notification.read
            ? t('admin.notifications.factReadYes')
            : t('admin.notifications.factReadNo')}
        </Fact>
      </FactList>

      <section class="block">
        <Heading level={4} variant="label">{t('admin.notifications.dangerTitle')}</Heading>
        <Text size="sm" tone="muted">{t('admin.notifications.retractHint')}</Text>
        <Button variant="destructive" disabled={busy} onclick={onRetract}>
          {t('admin.notifications.retract')}
        </Button>
      </section>
    </div>
  </Scroller>
</div>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .message {
    white-space: pre-wrap;
    word-break: break-word;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
    align-items: flex-start;
  }
</style>
