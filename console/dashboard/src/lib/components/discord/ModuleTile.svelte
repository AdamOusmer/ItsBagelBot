<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One module on the guild overview: what it is, whether it is on, whether it
  // can actually run, and one click to each of those.
  //
  // The switch is a form submit rather than a control feeding a page-wide
  // draft, and it posts ONE key. That is what makes the overview safe to flip
  // things from: `save` merges a partial draft, so a tile toggled here cannot
  // carry along a half-finished edit somebody left open on Channels. The flip
  // is optimistic and reverts on refusal, the same idiom MasterToggle uses.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    ButtonLink,
    Card,
    Chip,
    Icon,
    Switch,
    flagValue,
    getI18n,
    toast,
    type IconName,
    type ModuleTile
  } from '@bagel/shared';
  import { payloadOf, refusalTextOf, succeeded } from '$lib/discord/guild-draft.svelte';

  let {
    tile,
    guildId,
    version,
    icon,
    name,
    help
  }: {
    tile: ModuleTile;
    guildId: string;
    version: number;
    icon: IconName;
    name: string;
    help: string;
  } = $props();

  const { t } = getI18n();

  // null = "show what the server says". A boolean is the optimistic guess held
  // only for the round trip.
  let flipped = $state<boolean | null>(null);
  let pending = $state(false);

  const on = $derived(flipped ?? tile.on);
  // The value posted is the OPPOSITE of what is rendered, because the button is
  // the switch: by the time the form submits, the intent is the flip.
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
      toast('err', refusalTextOf(t, p, t('discord.toastSaveFailed')));
    };
  };

  const uid = $props.id();
  const helpId = `tile-help-${uid}`;
</script>

<Card>
  <div class="tile">
    <span class="ico" aria-hidden="true"><Icon name={icon} size={16} /></span>
    <div class="copy">
      <span class="name">{name}</span>
      <span class="help" id={helpId}>{help}</span>
    </div>
    <form method="POST" action="?/save" use:enhance={submit} class="flip">
      <input type="hidden" name="config" value={payload} />
      <input type="hidden" name="version" value={version} />
      <Switch type="submit" checked={on} {pending} label={name} describedby={helpId} />
    </form>
  </div>

  <div class="foot">
    <!-- On but missing a pick. Discord silently drops the post in that state,
         which from here is indistinguishable from the bot being broken. -->
    {#if on && !tile.ready}
      <Chip on>{t('discord.overview.needsSetup')}</Chip>
    {:else}
      <span class="spacer"></span>
    {/if}
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
  .ico {
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
  }
  .copy {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14px;
    color: var(--bb-white);
  }
  .help {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.45;
    color: var(--bb-muted);
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
    justify-content: space-between;
    gap: 10px;
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--glass-border);
  }
  /* Holds the row height steady whether or not the chip is there, so a grid of
     tiles does not jitter as modules are configured. */
  .spacer {
    display: block;
    min-height: 28px;
  }
</style>
