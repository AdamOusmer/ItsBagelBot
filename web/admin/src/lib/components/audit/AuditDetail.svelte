<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AuditEntry } from '$lib/server/services';
  import StatePill from '../StatePill.svelte';
  import { KIND_LABEL, auditKind } from './audit-kinds';

  let { entry }: { entry: AuditEntry } = $props();

  const { t } = getI18n();
  const kind = $derived(auditKind(entry.action));

  const when = $derived(new Date(entry.created_at).toLocaleString());
</script>

<div class="detail">
  <Scroller fill padding="18px" smooth>
    <Stack gap={4}>
      <Cluster gap={3} nowrap>
        <Bolota name={entry.actor_login} size={40} active />
        <div>
          <Heading level={6} as="div" variant="title">@{entry.actor_login}</Heading>
          <Text as="div" size="xs" mono tone="muted">{t('admin.audit.actorId', { id: String(entry.actor_id) })}</Text>
        </div>
      </Cluster>

      <Cluster gap={2}>
        <StatePill tone={entry.ok ? 'free' : 'banned'}>
          {entry.ok ? t('admin.audit.outcomeOk') : t('admin.audit.outcomeFailed')}
        </StatePill>
        <StatePill tone="neutral">{t(KIND_LABEL[kind])}</StatePill>
      </Cluster>

      <FactList>
        <Fact term={t('admin.audit.factAction')}>{entry.action}</Fact>
        <Fact term={t('admin.audit.factTarget')}>{entry.target || '-'}</Fact>
        <Fact term={t('admin.audit.factWhen')}>{when}</Fact>
        <Fact term={t('admin.audit.factEntry')}>#{entry.id}</Fact>
      </FactList>

      {#if entry.detail}
        <section class="block">
          <Heading level={3} variant="label">{t('admin.audit.factDetail')}</Heading>
          <div class="payload"><Text size="xs" mono tone="muted">{entry.detail}</Text></div>
        </section>
      {/if}

      {#if !entry.ok && entry.error}
        <section class="block">
          <Heading level={3} variant="label">{t('admin.audit.factError')}</Heading>
          <div class="payload"><Text size="xs" mono tone="danger">{entry.error}</Text></div>
        </section>
      {/if}
    </Stack>
  </Scroller>
</div>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .payload {
    overflow-wrap: anywhere;
  }
</style>
