<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The whole stored audit row, verbatim. No EditorFooter and no verbs: an
  // audit entry is immutable by design -- the trail is only worth reading if
  // nothing in this console can rewrite it -- so the inspector is pure display.
  //
  // `detail` and `error` are shown in full and wrap; they are the two fields the
  // row deliberately truncates, and truncating them here too would leave nowhere
  // to read them.
  import Scroller from '@bagel/kit/components/Scroller.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatePill from '../StatePill.svelte';
  import { KIND_LABEL, auditKind } from './audit-kinds';

  let { entry }: { entry: AuditEntry } = $props();

  const { t } = getI18n();
  const kind = $derived(auditKind(entry.action));

  // Absolute local timestamp: the row already carries the relative age, and an
  // audit entry is read to answer "at what time", which "2d ago" cannot.
  const when = $derived(new Date(entry.created_at).toLocaleString());
</script>

<div class="detail">
  <Scroller fill padding="18px" data-lenis-prevent>
    <div class="body">
      <div class="ident">
        <Bolota name={entry.actor_login} size={40} active />
        <div>
          <div class="name">@{entry.actor_login}</div>
          <div class="meta">{t('admin.audit.actorId', { id: String(entry.actor_id) })}</div>
        </div>
      </div>

      <div class="marks">
        <StatePill tone={entry.ok ? 'free' : 'banned'}>
          {entry.ok ? t('admin.audit.outcomeOk') : t('admin.audit.outcomeFailed')}
        </StatePill>
        <StatePill tone="neutral">{t(KIND_LABEL[kind])}</StatePill>
      </div>

      <dl class="facts">
        <div>
          <dt>{t('admin.audit.factAction')}</dt>
          <dd>{entry.action}</dd>
        </div>
        <div>
          <dt>{t('admin.audit.factTarget')}</dt>
          <dd>{entry.target || '-'}</dd>
        </div>
        <div>
          <dt>{t('admin.audit.factWhen')}</dt>
          <dd>{when}</dd>
        </div>
        <div>
          <dt>{t('admin.audit.factEntry')}</dt>
          <dd>#{entry.id}</dd>
        </div>
      </dl>

      {#if entry.detail}
        <section class="block">
          <h3 class="block-label">{t('admin.audit.factDetail')}</h3>
          <p class="payload">{entry.detail}</p>
        </section>
      {/if}

      {#if !entry.ok && entry.error}
        <section class="block">
          <h3 class="block-label">{t('admin.audit.factError')}</h3>
          <p class="payload err">{entry.error}</p>
        </section>
      {/if}
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
    gap: 16px;
  }

  .ident {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    margin-top: 2px;
  }

  .marks {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .facts {
    display: flex;
    flex-direction: column;
    gap: 7px;
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
    flex: none;
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-tan-light);
    text-align: right;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .payload {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0;
    overflow-wrap: anywhere;
  }
  .payload.err {
    color: var(--bb-status-error);
  }
</style>
