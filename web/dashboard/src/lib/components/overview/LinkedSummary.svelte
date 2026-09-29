<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let {
    active,
    commandsOk = true,
    modulesOn,
    modulesOk = true,
    planLabel,
    people,
    sharesOk = true
  }: {
    active: number;
    commandsOk?: boolean;
    modulesOn: number;
    modulesOk?: boolean;
    planLabel: string;
    people: number;
    sharesOk?: boolean;
  } = $props();

  type SummaryLink = { id: string; href: string; label: string };

  const links = $derived.by<SummaryLink[]>(() => [
    {
      id: 'commands',
      href: '/commands',
      label: !commandsOk
        ? t('overview.allCommands')
        : active > 0
          ? t('overview.summaryCommands', { active })
          : t('overview.summaryCommandsEmpty')
    },
    {
      id: 'modules',
      href: '/modules',
      label: !modulesOk
        ? t('overview.quickModules')
        : modulesOn > 0
          ? t('overview.summaryModules', { on: modulesOn })
          : t('overview.summaryModulesEmpty')
    },
    {
      id: 'plan',
      href: '/billing',
      label: t('overview.summaryPlan', { plan: planLabel })
    },
    {
      id: 'shares',
      href: '/settings',
      label: !sharesOk
        ? t('overview.manageInSettings')
        : people > 1
          ? t('overview.summaryShares', { n: people })
          : people === 1
            ? t('overview.summarySharesOne')
            : t('overview.summarySharesEmpty')
    }
  ]);
</script>

<section class="ov-summary" aria-labelledby="ov-summary-h">
  <Heading level={6} as="h2" variant="title" id="ov-summary-h">{t('overview.summaryHeading')}</Heading>
  <ul class="ov-summary__grid">
    {#each links as link (link.id)}
      <li>
        <ButtonLink href={link.href} variant="ghost">
          <span class="ov-summary__label">{link.label}</span>
        </ButtonLink>
      </li>
    {/each}
  </ul>
</section>

<style>
  .ov-summary {
    display: grid;
    gap: 12px;
    margin-bottom: var(--row-gap);
  }
  .ov-summary__grid {
    --btn-w: 100%;
    --btn-min-h: 56px;
    --btn-pad: 14px 18px;
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 10px;
  }
  .ov-summary__label {
    min-width: 0;
    white-space: normal;
  }

  @media (max-width: 560px) {
    .ov-summary__grid {
      grid-template-columns: 1fr;
    }
  }
</style>
