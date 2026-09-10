<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The read half of the notifications inspector: what was sent, to whom, and
  // the one verb a sent message still has (retract). No EditorFooter -- there is
  // nothing to save, because the notifications service has no update verb.
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
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

  // Absolute local timestamp, not `ago`: a sent notification is a record, and
  // "3d ago" is the wrong precision for deciding whether to retract one.
  function when(iso?: string): string {
    return iso ? new Date(iso).toLocaleString() : '';
  }
</script>

<div class="detail">
  <Scroller fill padding="18px" data-lenis-prevent>
    <div class="body">
      <div class="marks">
        <StatePill tone={LEVEL_TONE[notification.level]}>
          {t(LEVEL_LABEL[notification.level])}
        </StatePill>
        <StatePill tone="neutral">{t(audience.key, audience.params)}</StatePill>
      </div>

      <h3 class="title">{notification.title}</h3>
      <p class="message">{notification.body}</p>

      <dl class="facts">
        <div>
          <dt>{t('admin.notifications.factSentBy')}</dt>
          <dd>@{notification.created_by_login}</dd>
        </div>
        <div>
          <dt>{t('admin.notifications.factSentAt')}</dt>
          <dd>{when(notification.created_at)}</dd>
        </div>
        <div>
          <dt>{t('admin.notifications.factExpires')}</dt>
          <dd>
            {notification.expires_at
              ? when(notification.expires_at)
              : t('admin.notifications.factNoExpiry')}
          </dd>
        </div>
        <div>
          <dt>{t('admin.notifications.factRead')}</dt>
          <dd>
            {notification.read
              ? t('admin.notifications.factReadYes')
              : t('admin.notifications.factReadNo')}
          </dd>
        </div>
      </dl>

      <section class="block">
        <h4 class="block-label">{t('admin.notifications.dangerTitle')}</h4>
        <p class="note">{t('admin.notifications.retractHint')}</p>
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
  .marks {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    color: var(--bb-white);
    margin: 0;
  }
  .message {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.6;
    color: var(--bb-muted);
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .facts {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
  }
  .facts div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: baseline;
  }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    text-align: right;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
    align-items: flex-start;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 0;
  }
</style>
