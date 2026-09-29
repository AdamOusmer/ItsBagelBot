<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Disclosure from '@bagel/ui/svelte/Disclosure.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
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

<Disclosure size="sm" {summary} {open}>
  {#if commits.length > 0}
    <Scroller maxHeight="240px" role="region" aria-label={summary} tabindex={0}>
      <ul class="commits">
        {#each commits as c (c.sha)}
          <li class="commit">
            <Text as="span" size="xs" mono tone="muted">
              {#if c.url}<TextLink variant="inline" href={c.url} external>{shortSha(c.sha)}</TextLink>{:else}{shortSha(c.sha)}{/if}
            </Text>
            <span class="title"><Text as="span" size="sm" truncate>{c.title}</Text></span>
            <Text as="span" size="xs" mono tone="muted">{c.author}</Text>
          </li>
        {/each}
      </ul>
    </Scroller>
  {/if}
</Disclosure>

<style>
  .commits {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .commit {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    height: 28px;
  }
  .title {
    flex: 1;
    min-width: 0;
  }
</style>
