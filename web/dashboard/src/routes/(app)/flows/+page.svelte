<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import FlowThumb from '#lib/components/flow/FlowThumb.svelte';
  import FlowsIntro from '#lib/components/flow/FlowsIntro.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import { savedEnabled, savedPreviews } from '#lib/flow/session.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const { t } = getI18n();

  const SECTIONS = ['Commands', 'Alerts', 'Channel point rewards', 'Automations'];

  let introDismissed = $state(false);
  const showIntro = $derived(!data.introSeen && !introDismissed);

  let query = $state('');
  const needle = $derived(query.trim().toLowerCase());
  const groups = $derived(
    SECTIONS.map((title) => ({
      title,
      flows: data.flows.filter(
        (f) => f.section === title && (!needle || `${f.name} ${f.description} ${f.when}`.toLowerCase().includes(needle))
      )
    })).filter((g) => g.flows.length > 0)
  );
</script>

<svelte:head><title>{data.navLabels['nav.flows']} · ItsBagelBot</title></svelte:head>

{#snippet betaChip()}<Chip as="span" pressed>{t('modules.betaChip')}</Chip>{/snippet}

{#if data.locked}
  <PremiumGate />
{:else if showIntro}
  <FlowsIntro name={data.navLabels['nav.flows']} ondismiss={() => (introDismissed = true)} />
{:else}
<div class="landing" data-sveltekit-preload-code="eager">
  <PageHead
    eyebrow="Board · {data.navLabels['nav.flows']}"
    description="Pick what happens on stream, then lay out what your bot does next."
  >
    <span class="title">{data.navLabels['nav.flows']} {@render betaChip()}</span>
    {#snippet trailing()}
      <ButtonLink variant="primary" href="/flows/new"><Icon name="plus" size={16} /> New</ButtonLink>
    {/snippet}
  </PageHead>

  <div class="search"><SearchInput bind:value={query} placeholder="Search by name or trigger" fill /></div>

  {#each groups as group (group.title)}
    <section class="group">
      <h2 class="group-title">{group.title}</h2>
      <ul class="cards">
        {#each group.flows as flow (flow.id)}
          {@const enabled = savedEnabled.get(flow.id) ?? flow.enabled}
          <li class="card" class:off={!enabled}>
            <a class="open" href="/flows/{flow.id}">
              <span class="diagram"><FlowThumb preview={savedPreviews.get(flow.id) ?? flow.preview} /></span>
              <span class="name">{flow.name}</span>
              <span class="description">{flow.description}</span>
            </a>
            <div class="meta">
              <span class="facts">{flow.when}, {flow.steps} {flow.steps === 1 ? 'step' : 'steps'}</span>
              <Switch
                checked={enabled}
                label={enabled ? `Turn off ${flow.name}` : `Turn on ${flow.name}`}
                onCheckedChange={(next) => savedEnabled.set(flow.id, next)}
              />
            </div>
          </li>
        {/each}
      </ul>
    </section>
  {:else}
    <EmptyState title="Nothing matches" body="Try another name or trigger." />
  {/each}
</div>
{/if}

<style>
  .landing {
    display: flex;
    flex-direction: column;
    gap: 32px;
    min-width: 0;
  }
  .title {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
  .search {
    max-width: 420px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .group-title {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 15px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .cards {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
    gap: 16px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    height: 100%;
    padding: 16px;
    border-radius: var(--bb-radius-md);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.05), rgba(var(--bb-white-rgb), 0.02)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-1);
    transition: box-shadow var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .card:hover {
    box-shadow: var(--bb-depth-3);
  }
  .open {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    border-radius: var(--bb-radius-sm);
    color: inherit;
    text-decoration: none;
  }
  .open:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: 4px;
  }
  .diagram {
    display: block;
    margin-bottom: 10px;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .card.off .diagram {
    opacity: 0.4;
  }
  .name {
    font-family: var(--bb-font-body);
    font-size: 16px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .description {
    font-size: 14px;
    line-height: 1.4;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .facts {
    min-width: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .card,
    .diagram {
      transition: none;
    }
  }
</style>
