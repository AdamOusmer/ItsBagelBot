// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ModuleFieldType = 'text' | 'textarea' | 'number' | 'select' | 'toggle' | 'timezone';

export interface ModuleField {
  key: string;
  label: string;
  type: ModuleFieldType;
  placeholder?: string;
  help?: string;
  options?: { value: string; label: string }[];
  hidden?: boolean;
  followsLevel?: boolean;
}

export const AUTOMOD_LEVEL_DEFAULTS: Record<string, Record<string, boolean>> = {
  none: { harassment: false, sexual: false, profanity: false, style: false, links: false },
  basic: { harassment: true, sexual: false, profanity: false, style: false, links: false },
  moderate: { harassment: true, sexual: true, profanity: false, style: true, links: true },
  strict: { harassment: true, sexual: true, profanity: true, style: true, links: true }
};

export function automodToggleDefault(level: string, key: string): boolean {
  return (AUTOMOD_LEVEL_DEFAULTS[level] ?? AUTOMOD_LEVEL_DEFAULTS.moderate)[key] ?? false;
}

export interface ReplyToken {
  readonly name: string;
  readonly sample: string;
  readonly hintKey?: string;
}

export function replyTokens(
  names: readonly string[],
  samples: Readonly<Record<string, string>>,
  hintNamespace?: string
): ReplyToken[] {
  return names.map((name) => ({
    name,
    sample: samples[name] ?? '',
    ...(hintNamespace ? { hintKey: `replyVars.${hintNamespace}.${name.replace(/\./g, '_')}.hint` } : {})
  }));
}

export interface ModuleReply {
  key: string;
  label: string;
  tagline: string;
  event: string;
  messageKey: string;
  enableKey?: string;
  defaultOff?: boolean;
  defaultMessage: string;

  command?: string;
  previewArgs?: string;
  tokens?: readonly ReplyToken[];
}

export interface ModuleCommandInfo {
  trigger: string;
  summary: string;
  perm?: 'mod' | 'lead_mod';
  aliases?: string[];
}

export interface ModuleDef {
  id: string;
  label: string;
  tagline: string;
  description: string;
  category: string;
  defaultEnabled: boolean;
  toggleable?: boolean;
  hidden?: boolean;
  section?: boolean;
  beta?: boolean;
  replies: ModuleReply[];
  commands?: ModuleCommandInfo[];
  settings?: ModuleField[];
  parent?: string;
  href?: string;
  delegateSections?: readonly string[];
}

export function moduleDelegateSections(def: ModuleDef): readonly string[] {
  return def.delegateSections ?? ['modules'];
}

export interface ModuleState {
  def: ModuleDef;
  enabled: boolean;
  config: Record<string, string>;
  locked?: boolean;
}

export function betaLocked(def: ModuleDef, premium: boolean): boolean {
  return def.beta === true && !premium;
}
