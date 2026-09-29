<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployPRInfo } from '$lib/deploys/types';
  import CheckDot from './CheckDot.svelte';
  import { CHECK_KEY, checkTone, tickable } from './view';

  let {
    prs,
    picked = $bindable([])
  }: {
    prs: DeployPRInfo[];
    picked: number[];
  } = $props();

  const { t } = getI18n();

  function toggle(pr: DeployPRInfo, on: boolean) {
    picked = on ? [...picked, pr.number] : picked.filter((n) => n !== pr.number);
  }

  const checkLabel = (pr: DeployPRInfo) =>
    t('admin.deploys.prChecks', { state: t(CHECK_KEY[pr.checks]) });
  const codeSceneLabel = (pr: DeployPRInfo) =>
    t('admin.deploys.prCodeScene', { state: t(CHECK_KEY[pr.codescene]) });
</script>

{#if prs.length === 0}
  <Card glass flush>
    <div class="empty"><Text size="sm" tone="muted">{t('admin.deploys.prsEmpty')}</Text></div>
  </Card>
{:else}
  <DeckList as="ul" aria-label={t('admin.deploys.prs')}>
    {#each prs as pr (pr.number)}
      <ManagementRow as="li" selectable={false}>
        {#snippet primary()}
          <span class="pick" class:blocked={!tickable(pr)}>
            <Checkbox
              checked={picked.includes(pr.number)}
              disabled={!tickable(pr)}
              onchange={(e: Event) => toggle(pr, (e.currentTarget as HTMLInputElement).checked)}
            >
              <span class="pr-label">
                <Text as="span" size="xs" mono tone="muted">#{pr.number}</Text>
                <span class="title">{pr.title}</span>
              </span>
            </Checkbox>
          </span>
        {/snippet}
        {#snippet actions()}
          {#if pr.draft}<Tag tone="quiet">{t('admin.deploys.prDraft')}</Tag>{/if}
          {#if pr.behind}<Tag tone="quiet">{t('admin.deploys.prBehind')}</Tag>{/if}
          <span class="author"><Text as="span" size="xs" mono tone="muted">{pr.author}</Text></span>
          <CheckDot tone={checkTone(pr.checks)} label={checkLabel(pr)} />
          <CheckDot tone={checkTone(pr.codescene)} label={codeSceneLabel(pr)} />
          <Text as="span" size="xs" mono tone="muted"><TextLink variant="inline" href={pr.url} external>{t('admin.deploys.prOpen')}</TextLink></Text>
        {/snippet}
      </ManagementRow>
    {/each}
  </DeckList>
{/if}

<style>
  .empty {
    padding: var(--bb-space-2) var(--bb-space-4);
  }
  .pick {
    --bb-check-flex: 1;
    --bb-check-label-min-width: 0;
    display: flex;
    min-width: 0;
  }
  .pr-label {
    display: flex;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .title {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
  }
  .blocked .title {
    color: var(--bb-muted);
  }
  @media (max-width: 760px) {
    .author {
      display: none;
    }
  }
</style>
