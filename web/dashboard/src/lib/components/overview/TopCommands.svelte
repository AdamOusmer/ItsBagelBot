<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
	import '@bagel/ui/styles/elements/feed.css';
  import { formatCounterValue } from '@bagel/kit/validation';
  import { usesCount } from '@bagel/kit/uses';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { CommandView } from '@bagel/kit/types';

  const { t } = getI18n();

  let { top }: { top: CommandView[] } = $props();
</script>

<section class="ov-top" aria-labelledby="ov-top-h">
  <div class="ov-top__head">
    <Heading level={6} as="h2" variant="title" id="ov-top-h">{t('overview.topCommands')}</Heading>
    <TextLink href="/commands" label={t('overview.allCommands')} />
  </div>
  <Card>
    <ul class="bb-feed bb-stagger">
      {#each top as c (c.name)}
        <li class="bb-feed__row">
          <span class="bb-feed__body">
            <span class="bb-feed__title bb-feed__title--mono">!{c.name}</span>
            <span class="bb-feed__desc"><Text as="span" size="xs" tone="muted" truncate>{c.response}</Text></span>
          </span>
          <span class="bb-feed__trail">{t('overview.usesN', { n: formatCounterValue(usesCount(c).toString()) })}</span>
        </li>
      {/each}
      <li class="bb-feed__row">
        <span class="bb-feed__body">
          <span class="bb-feed__title">{t('overview.addAnother')}</span>
          <span class="bb-feed__desc">{t('overview.addAnotherDesc')}</span>
        </span>
        <span class="bb-feed__trail"><TextLink href="/commands" label={t('common.open')} /></span>
      </li>
    </ul>
  </Card>
</section>

<style>
  .ov-top {
    margin-bottom: var(--row-gap);
  }
  .ov-top__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
</style>
