<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let {
    receiving,
    hasCommands,
    modulesOn
  }: {
    receiving: boolean;
    hasCommands: boolean;
    modulesOn: boolean;
  } = $props();

  type Step = { id: string; label: string; href: string; done: boolean };

  const steps = $derived.by<Step[]>(() => [
    { id: 'connect', label: t('overview.setupConnect'), href: '/settings', done: receiving },
    { id: 'command', label: t('overview.setupCommand'), href: '/commands', done: hasCommands },
    { id: 'module', label: t('overview.setupModule'), href: '/modules', done: modulesOn }
  ]);

  const doneCount = $derived(steps.filter((s) => s.done).length);
</script>

<section class="ov-setup" aria-labelledby="ov-setup-h">
  <div class="ov-setup__head">
    <Heading level={6} as="h2" variant="title" id="ov-setup-h">{t('overview.setupHeading')}</Heading>
    <Text as="span" size="xs" mono tone="muted">{t('overview.setupProgress', { done: doneCount, total: steps.length })}</Text>
  </div>
  <Card>
    <ol class="ov-setup__list">
      {#each steps as step, i (step.id)}
        <li class="ov-setup__row" class:done={step.done}>
          <span class="ov-setup__ico" aria-hidden="true">
            {#if step.done}<Icon name="check" size={15} strokeWidth={1.7} />{:else}{i + 1}{/if}
          </span>
          <span class="ov-setup__label">
            <Text as="span" size="sm" tone={step.done ? 'muted' : 'default'}>{step.label}</Text>
          </span>
          {#if step.done}
            <Tag tone="live">{t('common.done')}</Tag>
          {:else}
            <ButtonLink href={step.href} variant="ghost">{t('common.open')}</ButtonLink>
          {/if}
        </li>
      {/each}
    </ol>
  </Card>
</section>

<style>
  .ov-setup {
    margin-bottom: var(--row-gap);
  }
  .ov-setup__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .ov-setup__list {
    --btn-min-h: 44px;
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .ov-setup__row {
    display: flex;
    align-items: center;
    gap: 13px;
    padding: 14px 2px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-setup__row:last-child {
    border-bottom: 0;
  }
  .ov-setup__ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border-radius: var(--bb-radius-sm);
    flex: none;
    background: rgba(var(--bb-tan-rgb), 0.1);
    border: 1px solid rgba(var(--bb-tan-rgb), 0.28);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
  }
  .ov-setup__row.done .ov-setup__ico {
    background: var(--bb-status-success-bg);
    border-color: var(--bb-status-success-border);
    color: var(--bb-status-success);
  }
  .ov-setup__label {
    flex: 1;
    min-width: 0;
  }
</style>
