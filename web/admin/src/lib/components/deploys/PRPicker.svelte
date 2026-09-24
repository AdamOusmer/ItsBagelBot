<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
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
  <p class="empty">{t('admin.deploys.prsEmpty')}</p>
{:else}
  <ul class="prs" aria-label={t('admin.deploys.prs')}>
    {#each prs as pr (pr.number)}
      <li class="pr-row" class:blocked={!tickable(pr)}>
        <Checkbox
          class="pick"
          checked={picked.includes(pr.number)}
          disabled={!tickable(pr)}
          onchange={(e: Event) => toggle(pr, (e.currentTarget as HTMLInputElement).checked)}
        >
          <span class="pr-label">
            <span class="num">#{pr.number}</span>
            <span class="title">{pr.title}</span>
          </span>
        </Checkbox>
        <span class="meta">
          {#if pr.draft}<Tag tone="quiet">{t('admin.deploys.prDraft')}</Tag>{/if}
          {#if pr.behind}<Tag tone="quiet">{t('admin.deploys.prBehind')}</Tag>{/if}
          <span class="author">{pr.author}</span>
          <CheckDot tone={checkTone(pr.checks)} label={checkLabel(pr)} />
          <CheckDot tone={checkTone(pr.codescene)} label={codeSceneLabel(pr)} />
          <a class="open" href={pr.url} target="_blank" rel="noopener noreferrer"
            >{t('admin.deploys.prOpen')}</a
          >
        </span>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty {
    margin: 0;
    font-size: 13px;
    color: var(--bb-muted);
  }
  .prs {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .pr-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 44px;
    padding: 6px 0;
    border-bottom: 1px solid var(--rule);
  }
  .pr-row:last-child {
    border-bottom: none;
  }
  .pr-row.blocked .title {
    color: var(--bb-muted);
  }
  .pr-row :global(.pick) {
    min-width: 0;
    --bb-check-flex: 1;
    --bb-check-label-min-width: 0;
  }
  .pr-label {
    display: flex;
    gap: 8px;
    min-width: 0;
  }
  .num {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
  }
  .title {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    font-size: 13.5px;
    line-height: 1.35;
  }
  .meta {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .author,
  .open {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }
  @media (max-width: 760px) {
    .author {
      display: none;
    }
  }
</style>
