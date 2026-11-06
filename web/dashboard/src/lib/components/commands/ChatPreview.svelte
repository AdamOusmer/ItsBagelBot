<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { translate, type Locale } from '@bagel/kit/i18n';
  import {
    rehearseCommand,
    rehearseReply,
    rehearseTimer,
    COMMAND_SAMPLES,
    normName,
    getI18n,
    type RehearsedLine
  } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';
  import ChatBotName from './ChatBotName.svelte';
  import RehearsedLineRow from './RehearsedLineRow.svelte';

  const i18n = getI18n();

  let {
    name = '',
    response = '',
    args = '',
    kind = 'command',
    showViewer = true,
    viewerText = undefined as string | undefined,
    tag = undefined as string | undefined,
    broadcasterName = undefined as string | undefined,
    locale = undefined as Locale | undefined,
    samples = undefined as Record<string, string> | undefined,
    dynamic = true
  }: {
    name?: string;
    response?: string;
    args?: string;
    kind?: 'command' | 'reply' | 'timer';
    showViewer?: boolean;
    viewerText?: string;
    tag?: string;
    broadcasterName?: string;
    locale?: Locale;
    samples?: Record<string, string>;
    dynamic?: boolean;
  } = $props();
  const t = (key: string, params?: Record<string, string | number>) => translate(locale ?? i18n.locale, key, params);

  const viewerName = $derived(samples?.user ?? Object.entries(samples ?? {}).find(([key]) => key.endsWith(':user'))?.[1] ?? COMMAND_SAMPLES.user);

  const botSeed = $derived(broadcasterName ?? (page.data.displayName as string | undefined) ?? 'ItsBagelBot');

  const trigger = $derived('!' + (normName(name) || 'command') + (args ? ' ' + args : ''));

  const moduleFlags = $derived((page.data.moduleFlags as Record<string, boolean> | undefined) ?? {});

  const views = $derived.by<RehearsedLine[]>(() => {
    if (kind === 'command') return rehearseCommand(response, samples, moduleFlags);
    if (kind === 'timer') return rehearseTimer(response, moduleFlags);
    return rehearseReply(response, samples, { dynamic });
  });

  const effectiveShowViewer = $derived(kind === 'timer' ? false : showViewer);

  let typing = $state(false);
  let hovered = $state(false);
  let settle: ReturnType<typeof setTimeout> | undefined;
  let first = true;
  $effect(() => {
    void response;
    void name;
    if (first) {
      first = false;
      return;
    }
    typing = true;
    clearTimeout(settle);
    settle = setTimeout(() => (typing = false), 550);
    return () => clearTimeout(settle);
  });
</script>

<ChatPreview
  title={tag ?? t('chatPreview.rehearsal')}
  label={t('chatPreview.ariaPreview')}
  class="chat-preview"
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  {#if effectiveShowViewer}
    <div class="bb-chat__line">
      <span class="bb-chat__name bb-chat__name--viewer">{viewerName}</span>
      <span class="bb-chat__msg" class:bb-chat__msg--cmd={viewerText === undefined}>{viewerText ?? trigger}</span>
    </div>
  {/if}
  {#if typing}
    <div class="bb-chat__line">
      <ChatBotName seed={botSeed} active={typing || hovered} />
      <span class="bb-chat__msg bb-chat__msg--typing" aria-label={t('chatPreview.ariaTyping')}>
        <span class="bb-drawline" aria-hidden="true"></span>
      </span>
    </div>
  {:else if views.length === 0}
    <div class="bb-chat__line">
      <ChatBotName seed={botSeed} active={typing || hovered} />
      <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nothingToSay')}</span>
    </div>
  {:else}
    {#each views as v, li (li)}
      <RehearsedLineRow line={v} index={li} seed={botSeed} active={typing || hovered} {locale} />
    {/each}
  {/if}
</ChatPreview>

<style>
  :global(.chat-preview) { margin-top: 10px; }
</style>
