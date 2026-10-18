<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import {
    discordModuleToggleable,
    getI18n,
    tDiscordModuleLabel,
    tDiscordModuleTagline
  } from '@bagel/kit';
  import type { Snippet } from 'svelte';
  import type { ModuleDraft } from '$lib/discord/module-draft.svelte';

  let {
    draft,
    guildId,
    children,
    footer
  }: { draft: ModuleDraft; guildId: string; children: Snippet; footer?: Snippet } = $props();

  const { t } = getI18n();

  const def = $derived(draft.def);
  const label = $derived(tDiscordModuleLabel(t, def));
  const toggleable = $derived(discordModuleToggleable(def));
  const hasFields = $derived(def.fields.length > 0);
</script>

<section class="module">
  <nav class="crumbs" aria-label={t('discord.module.breadcrumbLabel')}>
    <ol>
      <li><TextLink variant="quiet" touch icon="arrowLeft" href="/discord/{guildId}" label={t('discord.module.allModules')} /></li>
      <li class="crumb-sep" aria-hidden="true"><Text as="span" size="sm" tone="muted">/</Text></li>
      <li><Text as="span" size="sm" tone="accent" aria-current="page">{label}</Text></li>
    </ol>
  </nav>

  <PageHead eyebrow={t('discord.module.eyebrow')} description={tDiscordModuleTagline(t, def)}>{label}</PageHead>

  {#if draft.conflicted}
    <AlertBanner tone="warning">
      {t('discord.conflictBody')}
      {#snippet actions()}
        <Button variant="secondary" onclick={draft.reload}>{t('discord.conflictCta')}</Button>
      {/snippet}
    </AlertBanner>
  {/if}

  {#if draft.invalidBanner}
    <AlertBanner tone="warning">{draft.invalidBanner}</AlertBanner>
  {/if}

  <div>
    <Card flush>
      <div class="master-row">
        <div class="tr-text">
          <Heading level={6} as="h2">{t('discord.module.statusTitle')}</Heading>
          <Text as="span" size="xs" tone="muted">
            {toggleable ? t('discord.module.statusHelp') : t('discord.module.coreHelp')}
          </Text>
        </div>
        {#if toggleable}
          <Tag tone={draft.enabled ? 'live' : 'quiet'} mark={draft.enabled ? 'solid' : 'hollow'}>
            {draft.enabled ? t('discord.module.statusOn') : t('discord.module.statusOff')}
          </Tag>
          <SaveStatus state={draft.toggleState} />
          <form method="POST" action="?/toggle" use:enhance={draft.toggleSubmit} class="flip">
            <input type="hidden" name="enabled" value={draft.enabled ? 'off' : 'on'} />
            <input type="hidden" name="version" value={draft.version} />
            <Switch
              type="submit"
              checked={draft.enabled}
              label={t('discord.module.toggleAria', { label })}
              busy={draft.toggleState === 'saving'}
            />
          </form>
        {:else}
          <Tag tone="pre">{t('discord.module.alwaysOn')}</Tag>
        {/if}
      </div>
      {#if toggleable}
        <div class="disabled-note" data-shown={!draft.enabled || undefined} aria-hidden={draft.enabled}>
          <Text size="sm" tone="muted">{t('discord.module.disabledNote')}</Text>
        </div>
      {/if}
    </Card>
  </div>

  <section class="block" aria-labelledby="dc-module-settings-h">
    <Heading level={6} as="h2" variant="title" id="dc-module-settings-h" class="block-title">{t('discord.module.settingsTitle')}</Heading>
    <Card>
      {#if hasFields}
        <form method="POST" action="?/save" use:enhance={draft.saveSubmit} novalidate>
          <input type="hidden" name="settings" value={draft.payload} />
          <input type="hidden" name="version" value={draft.version} />

          {@render children()}

          <div class="actions">
            <SaveStatus state={draft.saveState} />
            <Button
              variant="primary"
              type="submit"
              busy={draft.saveState === 'saving'}
              disabled={draft.toggleState === 'saving'}
            >
              {t('discord.save')}
            </Button>
          </div>
        </form>
      {:else}
        <Text size="sm" tone="muted" class="hint">{t('discord.module.noSettings')}</Text>
      {/if}

      {#if footer}{@render footer()}{/if}
    </Card>
  </section>
</section>

<ConfirmDialog
  open={draft.discardOpen}
  title={t('discord.unsavedTitle')}
  body={t('discord.unsavedBody')}
  confirmLabel={t('discord.unsavedConfirm')}
  cancelLabel={t('common.cancel')}
  onConfirm={draft.confirmDiscard}
  onCancel={draft.cancelDiscard}
  tone="danger"
/>

<style>
  .module {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .crumbs ol {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .crumb-sep {
    opacity: 0.5;
  }
  .master-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 18px;
  }
  .tr-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
    margin-right: auto;
    min-width: 0;
  }
  .flip {
    display: flex;
    align-items: center;
  }
  .disabled-note {
    padding: 12px 18px 16px;
    border-top: 1px solid var(--bb-border);
    visibility: hidden;
  }
  .disabled-note[data-shown] {
    visibility: visible;
  }
</style>
