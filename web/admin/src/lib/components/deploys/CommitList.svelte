<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployCommit } from '$lib/deploys/types';
  import { shortSha } from './view';
  import { countKey } from './ship';

  let { commits, since, open = false }: { commits: DeployCommit[]; since: string; open?: boolean } = $props();

  const { t } = getI18n();

  const summary = $derived(
    commits.length === 0
      ? t('admin.deploys.commitsNone', { tag: since })
      : t(countKey(commits.length, 'admin.deploys.commits'), { n: String(commits.length), tag: since })
  );
</script>

<details class="commits" {open}>
  <summary>{summary}</summary>
  {#if commits.length > 0}
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div class="scroll" role="region" aria-label={summary} tabindex="0">
    <ul>
      {#each commits as c (c.sha)}
        <li>
          {#if c.url}
            <a class="sha" href={c.url} target="_blank" rel="noopener noreferrer">{shortSha(c.sha)}</a>
          {:else}
            <span class="sha">{shortSha(c.sha)}</span>
          {/if}
          <span class="title">{c.title}</span>
          <span class="author">{c.author}</span>
        </li>
      {/each}
    </ul>
    </div>
  {/if}
</details>

<style>
  .commits summary {
    cursor: pointer;
    height: 32px;
    line-height: 32px;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.04em;
    color: var(--bb-muted);
  }
  .scroll {
    max-height: 240px;
    overflow-y: auto;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 28px;
    font-size: 12.5px;
  }
  .sha,
  .author {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    flex-shrink: 0;
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
