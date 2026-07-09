<script lang="ts">
  // A read-only command row on a module page: the static twin of ReplyRow. It
  // lists a chat command the module unlocks (trigger + summary), with a small
  // "Mods" tag when the command is moderator-only. Unlike ReplyRow it is not a
  // button and carries no toggle or inspector: nothing here is editable, so it
  // is deliberately not clickable.
  import { getI18n, type ModuleCommandInfo } from '@bagel/shared';

  const { t } = getI18n();

  let { command, index }: { command: ModuleCommandInfo; index: number } = $props();

  const idx = $derived(String(index).padStart(2, '0'));
</script>

<div class="row-shell" style="--i: {index - 1}">
  <div class="trow">
    <span class="idx" aria-hidden="true">{idx}</span>
    <span class="cmd">
      <span class="cmd-name"><code>{command.trigger}</code></span>
      <span class="resp">{command.summary}</span>
    </span>
    {#if command.perm === 'mod'}
      <span class="tag">{t('modules.permMods')}</span>
    {:else}
      <span class="mini-spacer" aria-hidden="true"></span>
    {/if}
  </div>
</div>

<style>
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
  }
  .trow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    padding: 13px 14px;
    user-select: none;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .cmd-name code {
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 13px;
    color: var(--bb-white);
  }
  .resp {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .tag {
    font-family: var(--bb-font-mono);
    font-size: 9.5px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-tan);
    border: 1px solid var(--rule-tan, rgba(201, 168, 124, 0.3));
    border-radius: 4px;
    padding: 2px 6px;
    white-space: nowrap;
  }
  .mini-spacer { width: 0; }

  @media (max-width: 760px) {
    .trow { grid-template-columns: minmax(0, 1fr) auto; }
    .idx { display: none; }
  }
</style>
