// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type ModuleReply } from '@bagel/kit';
import { toast } from '@bagel/ui/svelte/toast';

export type Match = 'word' | 'contains' | 'exact' | 'prefix';
export type Rule = { phrase: string; response: string; match: Match; enabled: boolean };
type RuleKey = string;
type RuleSource = string;
export type RuleOutcome = 'saved' | 'conflict' | 'failed';

export type RuleText = {
  newRule(): string;
  match(match: Match): string;
  event(rule: Rule): string;
  saveFailed(): string;
  couldNotToggle(rule: Rule): string;
  saved(): string;
};

export type RuleHost = {
  expanded: string | null;
  editMessage: string;
  busy: boolean;
  readonly locked: boolean;
  readonly text: RuleText;
  guard(run: () => void): void;
  closeInspector(): void;
  doClose(): void;
  setSaving(key: RuleKey): void;
  ackSaved(key: RuleKey): void;
  flagError(key: RuleKey): void;
  persist(serialized: RuleSource): Promise<RuleOutcome>;
};

export const MATCH_KEY = Object.freeze<Record<Match, string>>({
  word: 'modules.matchWord',
  contains: 'modules.matchContains',
  exact: 'modules.matchExact',
  prefix: 'modules.matchPrefix'
});

const TRIGGER_TOKENS = { tokens: [{ name: 'triggers:user', sample: '' }, { name: 'triggers:channel', sample: '' }] };

const isMatch = (value: unknown): value is Match => typeof value === 'string' && Object.hasOwn(MATCH_KEY, value);

const namespaceTrigger = (message: RuleSource) => namespaceReplyTemplate('triggers', TRIGGER_TOKENS, message);

function splitMode(left: RuleSource): [Match, string] {
  const colon = left.indexOf(':');
  if (colon < 0) return ['word', left];
  const prefix = left.slice(0, colon).trim().toLowerCase();
  return isMatch(prefix) ? [prefix, left.slice(colon + 1).trim()] : ['word', left];
}

function parseLine(raw: RuleSource): Rule | null {
  const trimmed = raw.trim();
  const commented = trimmed.startsWith('#');
  const line = commented ? trimmed.slice(1).trim() : trimmed;
  const sep = line.indexOf('=>');
  if (sep < 0) return null;
  const [match, phrase] = splitMode(line.slice(0, sep).trim());
  const response = line.slice(sep + 2).trim();
  return phrase && response ? { phrase, response, match, enabled: !commented } : null;
}

function parseJSONRules(s: RuleSource): Rule[] {
  try {
    const arr = JSON.parse(s);
    if (!Array.isArray(arr)) return [];
    return (arr as Array<Record<string, unknown>>)
      .map((r) => ({
        phrase: String(r.phrase ?? ''),
        response: String(r.response ?? ''),
        match: isMatch(r.match) ? r.match : ('word' as Match),
        enabled: r.enabled !== false
      }))
      .filter((r) => r.phrase.trim() && r.response.trim());
  } catch {
    return [];
  }
}

export function parseRules(raw: RuleSource): Rule[] {
  const s = raw.trim();
  if (!s) return [];
  if (s[0] === '[') return parseJSONRules(s);
  return raw.split('\n').map(parseLine).filter((rule): rule is Rule => rule !== null);
}

export function serializeRules(list: Rule[]): string {
  return JSON.stringify(
    list
      .filter((r) => r.phrase.trim() && r.response.trim())
      .map((r) => ({
        phrase: r.phrase.trim(),
        response: r.response.replace(/\s*\n\s*/g, ' ').trim(),
        match: r.match,
        enabled: r.enabled
      }))
  );
}

export class TriggerRules {
  rules = $state<Rule[]>([]);
  index = $state<number | null>(null);
  phrase = $state('');
  match = $state<Match>('word');
  deleteIndex = $state<number | null>(null);
  private host: RuleHost;

  constructor(raw: RuleSource, host: RuleHost) {
    this.host = host;
    this.rules = parseRules(raw);
  }

  reset(raw: RuleSource) {
    this.rules = parseRules(raw);
  }

  get deletePhrase(): string {
    return this.deleteIndex === null ? '' : (this.rules[this.deleteIndex]?.phrase ?? '');
  }

  rows(): ModuleReply[] {
    const text = this.host.text;
    return this.rules.map((r, i) => ({
      key: `rule:${i}`,
      label: r.phrase || text.newRule(),
      tagline: text.match(r.match),
      event: text.event(r),
      messageKey: `rule:${i}`,
      enableKey: `rule:${i}`,
      defaultMessage: r.response
    }));
  }

  dirty(): boolean {
    const i = this.index;
    if (i === null) return false;
    const message = this.host.editMessage;
    if (i === -1) return this.phrase.trim() !== '' || message.trim() !== '';
    const r = this.rules[i];
    return !r || this.phrase !== r.phrase || this.match !== r.match || message !== namespaceTrigger(r.response);
  }

  open(i: number) {
    const host = this.host;
    if (host.locked) return;
    if (host.expanded === `rule:${i}`) return host.closeInspector();
    host.guard(() => {
      const r = this.rules[i];
      this.index = i;
      this.phrase = r.phrase;
      this.match = r.match;
      host.editMessage = namespaceTrigger(r.response);
      host.expanded = `rule:${i}`;
    });
  }

  add() {
    const host = this.host;
    if (host.locked) return;
    host.guard(() => {
      this.index = -1;
      this.phrase = '';
      this.match = 'word';
      host.editMessage = '';
      host.expanded = 'rule:new';
    });
  }

  async save() {
    const host = this.host;
    const index = this.index;
    if (index === null) return;
    host.editMessage = namespaceTrigger(host.editMessage);
    const keepOn = index === -1 ? true : (this.rules[index]?.enabled ?? true);
    const draft: Rule = { phrase: this.phrase.trim(), response: host.editMessage, match: this.match, enabled: keepOn };
    const next = index === -1 ? [...this.rules, draft] : this.rules.map((r, i) => (i === index ? draft : r));
    await this.commit(next, host.expanded ?? 'rule', {
      busy: true,
      onSaved: () => {
        if (this.index === -1) {
          this.index = next.length - 1;
          host.expanded = `rule:${next.length - 1}`;
        }
        this.toastSaved();
      },
      failText: () => host.text.saveFailed()
    });
  }

  askDelete() {
    if (this.index !== null && this.index >= 0) this.deleteIndex = this.index;
    else this.host.closeInspector();
  }

  confirmDelete() {
    const i = this.deleteIndex;
    this.deleteIndex = null;
    if (i !== null) void this.remove(i);
  }

  async toggle(i: number) {
    const next = this.rules.map((r, idx) => (idx === i ? { ...r, enabled: !r.enabled } : r));
    const host = this.host;
    await this.commit(next, `rule:${i}`, {
      onSaved: () => host.ackSaved(`rule:${i}`),
      failText: () => host.text.couldNotToggle(this.rules[i])
    });
  }

  private async remove(i: number) {
    const host = this.host;
    await this.commit(
      this.rules.filter((_, idx) => idx !== i),
      `rule:${i}`,
      {
        busy: true,
        onSaved: () => {
          if (host.expanded === `rule:${i}`) host.doClose();
          this.toastSaved();
        },
        failText: () => host.text.saveFailed()
      }
    );
  }

  private toastSaved() {
    toast('success', this.host.text.saved());
  }

  private async commit(
    next: Rule[],
    key: RuleKey,
    outcomeHandlers: { busy?: boolean; onSaved: () => void; failText: () => string }
  ) {
    const host = this.host;
    if (outcomeHandlers.busy) host.busy = true;
    host.setSaving(key);
    const outcome = await host.persist(serializeRules(next));
    if (outcomeHandlers.busy) host.busy = false;
    if (outcome === 'saved') {
      this.rules = next;
      outcomeHandlers.onSaved();
      return;
    }
    host.flagError(key);
    if (outcome === 'failed') toast('danger', outcomeHandlers.failText());
  }
}
