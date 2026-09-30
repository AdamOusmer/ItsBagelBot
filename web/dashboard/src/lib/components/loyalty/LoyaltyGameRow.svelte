<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Heading,
    ManagementRow,
    Switch,
    Tag,
    Text,
    toast
  } from '@bagel/ui/svelte';
  import {
    getI18n,
    tModuleLabel,
    tModuleTagline,
    type ModuleDef
  } from '@bagel/kit';

  let {
    def,
    enabled = $bindable(false),
    loyaltyOn
  }: {
    def: ModuleDef;
    enabled: boolean;
    loyaltyOn: boolean;
  } = $props();

  const { t } = getI18n();
  const href = $derived(def.href ?? `/modules/${def.id}`);
  const chips = $derived.by(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const command of def.commands ?? []) {
      const token = command.trigger.trim().split(/\s+/)[0] ?? '';
      const key = token.toLowerCase();
      if (!token || seen.has(key)) continue;
      seen.add(key);
      out.push(token);
      if (out.length >= 2) break;
    }
    return out;
  });
  let pending = $state(false);

  const submit: SubmitFunction = () => {
    if (!loyaltyOn) return;
    const was = enabled;
    enabled = !was;
    pending = true;
    return async ({ result }) => {
      pending = false;
      if (result.type !== 'success') {
        enabled = was;
        toast('danger', t('loyalty.toastGameToggleFailed', { label: tModuleLabel(t, def) }));
      }
    };
  };
</script>

<ManagementRow as="article" {href} id={def.id} class="loyalty-game">
  {#snippet primary()}
    <span class="copy">
      <Heading level={6} as="span">{tModuleLabel(t, def)}</Heading>
      <span class="tagline"><Text as="span" size="xs" tone="muted">{tModuleTagline(t, def)}</Text></span>
      {#if chips.length}
        <span class="cmds">
          {#each chips as chip (chip)}
            <Tag bare literal>{chip}</Tag>
          {/each}
        </span>
      {/if}
    </span>
  {/snippet}
  {#snippet actions()}
    <form class="game-toggle" method="POST" action="?/toggleGame" use:enhance={submit}>
      <input type="hidden" name="name" value={def.id} />
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <Switch
        type="submit"
        checked={enabled}
        disabled={!loyaltyOn}
        busy={pending}
        label={enabled ? t('modules.disableAria', { label: tModuleLabel(t, def) }) : t('modules.enableAria', { label: tModuleLabel(t, def) })}
      />
    </form>
  {/snippet}
</ManagementRow>

<style>
  .copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .tagline {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .cmds { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 6px; }

  @media (max-width: 560px) {
    :global(.loyalty-game) { flex-wrap: wrap; justify-content: flex-end; }
    .game-toggle { padding-bottom: 8px; }
  }
</style>
