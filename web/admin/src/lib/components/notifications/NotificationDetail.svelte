<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NotificationWire } from '#lib/server/services';
  import StatePill from '../StatePill.svelte';
  import { LEVEL_LABEL, LEVEL_TONE, audienceOf } from './notification-compose';

  let { notification, canSend, busy, onRetract }: {
    notification: NotificationWire;
    canSend: boolean;
    busy: boolean;
    onRetract: () => void;
  } = $props();

  const { t, locale } = getI18n();
  const dateTime = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });
  const audience = $derived(audienceOf(notification));
</script>

<Scroller fill padding="var(--bb-space-4)">
  <Stack gap={5}>
    <Stack gap={3}>
      <Cluster gap={2}>
        <StatePill tone={LEVEL_TONE[notification.level]}>
          {t(LEVEL_LABEL[notification.level])}
        </StatePill>
        <StatePill tone="neutral">{t(audience.key, audience.params)}</StatePill>
      </Cluster>
      <Heading level={2} variant="title" class="notification-title">{notification.title}</Heading>
      <div class="message"><Text size="sm">{notification.body}</Text></div>
    </Stack>

    <FactList>
      <Fact fit term={t('admin.notifications.factSentBy')}>@{notification.created_by_login}</Fact>
      <Fact fit term={t('admin.notifications.factSentAt')}>
        {dateTime.format(new Date(notification.created_at))}
      </Fact>
      <Fact fit term={t('admin.notifications.factExpires')}>
        {notification.expires_at
          ? dateTime.format(new Date(notification.expires_at))
          : t('admin.notifications.factNoExpiry')}
      </Fact>
    </FactList>

    {#if canSend}
      <Stack gap={2} align="start">
        <Text size="sm" tone="muted">{t('admin.notifications.retractHint')}</Text>
        <Button variant="secondary" disabled={busy} onclick={onRetract} tone="danger">
          {t('admin.notifications.retract')}
        </Button>
      </Stack>
    {/if}
  </Stack>
</Scroller>

<style>
  :global(.notification-title) {
    overflow-wrap: anywhere;
  }

  .message {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
