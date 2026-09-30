<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Spacer from '@bagel/ui/svelte/Spacer.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { flagValue, getI18n, type ModuleTile } from '@bagel/kit';
  import { payloadOf, refusalTextOf, succeeded } from '$lib/discord/guild-draft.svelte';

  let {
    tile,
    guildId,
    version,
    name,
    help
  }: {
    tile: ModuleTile;
    guildId: string;
    version: number;
    name: string;
    help: string;
  } = $props();

  const { t } = getI18n();

  let flipped = $state<boolean | null>(null);
  let pending = $state(false);

  const on = $derived(flipped ?? tile.on);
  const payload = $derived(JSON.stringify({ [tile.flag]: flagValue(!on) }));

  const submit: SubmitFunction = () => {
    flipped = !on;
    pending = true;
    return async ({ result }) => {
      pending = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        await invalidateAll();
        flipped = null;
        return;
      }
      flipped = null;
      toast('danger', refusalTextOf(t, p, t('discord.toast.saveFailed')));
    };
  };

  const uid = $props.id();
  const helpId = `tile-help-${uid}`;
</script>

<Card>
  <div class="tile">
    <div class="copy">
      <Heading level={6} as="span">{name}</Heading>
      <Text as="span" size="xs" tone="muted" id={helpId}>{help}</Text>
    </div>
    <form method="POST" action="?/save" use:enhance={submit} class="flip">
      <input type="hidden" name="config" value={payload} />
      <input type="hidden" name="version" value={version} />
      <Switch type="submit" checked={on} busy={pending} label={name} describedby={helpId} />
    </form>
  </div>

  <div class="foot">
    {#if on && !tile.ready}
      <Tag tone="warning">{t('discord.overview.needsSetup')}</Tag>
    {/if}
    <Spacer grow />
    <ButtonLink variant="ghost" href="/discord/{guildId}{tile.href}">
      {t('discord.overview.configure')}
    </ButtonLink>
  </div>
</Card>

<style>
  .tile {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .copy {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .flip {
    flex: none;
    display: flex;
    align-items: center;
    padding-top: 2px;
  }

  .foot {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--bb-glass-border);
  }
</style>
