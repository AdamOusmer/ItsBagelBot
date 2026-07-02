<script lang="ts">
  // Alternate-name (alias) chip input. Commits on Enter/comma/blur, pops the
  // last chip on Backspace in an empty input, de-duplicates case-insensitively
  // against the command's own name and existing chips.
  import { Icon } from '@bagel/shared';

  let {
    aliases = $bindable<string[]>([]),
    draft = $bindable(''),
    commandName = ''
  }: {
    aliases: string[];
    draft: string;
    commandName?: string;
  } = $props();

  export function commit(): void {
    const a = draft.trim();
    draft = '';
    if (!a) return;
    const key = a.toLowerCase();
    if (key === commandName.trim().toLowerCase()) return;
    if (aliases.some((x) => x.toLowerCase() === key)) return;
    aliases = [...aliases, a];
  }

  function remove(alias: string) {
    aliases = aliases.filter((a) => a !== alias);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit();
    } else if (e.key === 'Backspace' && draft === '' && aliases.length) {
      aliases = aliases.slice(0, -1);
    }
  }
</script>

<input
  class="search"
  placeholder="Type a name, press Enter"
  bind:value={draft}
  onkeydown={onKey}
  onblur={commit}
/>
{#if aliases.length}
  <div class="pills">
    {#each aliases as a (a)}
      <button type="button" class="pill" onclick={() => remove(a)} aria-label="Remove {a}">
        <span>{a}</span>
        <Icon name="x" size={11} />
      </button>
    {/each}
  </div>
{/if}

<style>
  .pills { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: 999px;
    padding: 3px 10px;
    cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .pill :global(svg) {
    width: 0;
    opacity: 0;
    transition: width var(--bb-dur-fast, 140ms) ease, opacity var(--bb-dur-fast, 140ms) ease;
  }
  .pill:hover, .pill:focus-visible {
    color: #cf8a78;
    background: rgba(176, 90, 70, 0.16);
    border-color: rgba(176, 90, 70, 0.45);
    outline: none;
  }
  .pill:hover :global(svg), .pill:focus-visible :global(svg) {
    width: 11px;
    opacity: 1;
  }

  input.search { width: 100%; box-sizing: border-box; }
</style>
