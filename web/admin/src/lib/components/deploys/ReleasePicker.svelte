<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployReleaseInfo } from '$lib/deploys/types';
  import { shortSha } from './view';

  let { releases, value = $bindable('') }: { releases: DeployReleaseInfo[]; value: string } = $props();

  const { t } = getI18n();
</script>

{#if releases.length === 0}
  <p class="empty">{t('admin.deploys.flow.releasesEmpty')}</p>
{:else}
  <div class="releases" role="radiogroup" aria-label={t('admin.deploys.rollbackTo')}>
    {#each releases as r (r.version)}
      <label class="release" class:selected={value === r.version}>
        <input type="radio" name="ship-release" value={r.version} bind:group={value} />
        <span class="version">{r.version}</span>
        <span class="sha">{shortSha(r.sha)}</span>
        <span class="when">{ago(r.published_at)}</span>
      </label>
    {/each}
  </div>
{/if}

<style>
  .empty {
    margin: 0;
    font-size: 13px;
    color: var(--bb-muted);
  }
  .releases {
    display: flex;
    flex-direction: column;
    max-height: 360px;
    overflow-y: auto;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
  }
  .release {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 12px;
    min-height: 44px;
    padding: 0 14px;
    border-bottom: 1px solid var(--bb-border);
    cursor: pointer;
    transition: background 240ms ease;
  }
  .release:last-child {
    border-bottom: none;
  }
  .release:hover {
    background: rgba(255, 255, 255, 0.03);
  }
  .release.selected {
    background: linear-gradient(100deg, rgba(var(--bb-tan-rgb), 0.14), rgba(var(--bb-green-glow-rgb), 0.06));
  }
  input {
    accent-color: var(--bb-green-glow);
    margin: 0;
  }
  input:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: 2px;
  }
  .version {
    font: 700 14px/1.2 var(--bb-font-display);
  }
  .sha,
  .when {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }
  @media (prefers-reduced-motion: reduce) {
    .release {
      transition: none;
    }
  }
</style>
