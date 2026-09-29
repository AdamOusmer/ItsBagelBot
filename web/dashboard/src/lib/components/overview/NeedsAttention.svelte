<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let {
    active,
    total,
    commandsOk = true,
    pendingShares = 0,
    sharesOk = true
  }: {
    active: number;
    total: number;
    commandsOk?: boolean;
    pendingShares?: number;
    sharesOk?: boolean;
  } = $props();

  type Issue = { id: string; text: string; cta: string; href: string };

  const issues = $derived.by<Issue[]>(() => {
    const out: Issue[] = [];
    if (commandsOk && total > 0 && active === 0) {
      out.push({
        id: 'all-disabled',
        text: t('overview.issueAllDisabled'),
        cta: t('overview.issueAllDisabledCta'),
        href: '/commands'
      });
    }
    if (sharesOk && pendingShares > 0) {
      out.push({
        id: 'pending-invites',
        text: t('overview.invitesPending', { n: pendingShares }),
        cta: t('overview.manageInSettings'),
        href: '/settings'
      });
    }
    return out;
  });
</script>

{#if issues.length}
  <section class="ov-attention" aria-labelledby="ov-attention-h">
    <Heading level={6} as="h2" variant="title" id="ov-attention-h">{t('overview.attentionHeading')}</Heading>
    <ul class="ov-attention__list">
      {#each issues as issue (issue.id)}
        <li>
          <AlertBanner variant="warn" flush role="note" stack>
            {issue.text}
            {#snippet action()}
              <ButtonLink href={issue.href} variant="ghost">{issue.cta}</ButtonLink>
            {/snippet}
          </AlertBanner>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .ov-attention {
    display: grid;
    gap: 12px;
    margin-bottom: var(--row-gap);
  }
  .ov-attention__list {
    --btn-min-h: 44px;
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
</style>
