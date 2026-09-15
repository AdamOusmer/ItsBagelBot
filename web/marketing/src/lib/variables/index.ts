// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The exhaustive, broadcaster-facing variable reference.
 *
 * `builder.ts` remains the source for the examples used by the command
 * builder. This module flattens those surface-local examples into one small
 * reference model: a variable family appears once, while its aliases,
 * parameter shapes and available surfaces are collected beside it.
 *
 * This is deliberately not a resolver. An unknown `{anything}` is still a
 * perfectly valid token to the lexer and is left literal by the bot. Runtime
 * ownership belongs to the scopes; this catalog describes the public forms
 * that the marketing site promises and uses the shared lexer to check them.
 */

import { lex, type VarToken } from '@bagel/kit/engine/tmpl';
import { SURFACES } from '../../i18n/builder';
import type { Lang } from '../../i18n/ui';

// `builder.ts` accepts the site's open locale set (`Lang` is intentionally an
// open string), so the catalog keeps the same shape while always shipping the
// en/fr source copy.
export type LocaleText = Readonly<Record<string, string>>;

export type VariableCategory =
  | 'basics'
  | 'arguments'
  | 'counters'
  | 'dynamic'
  | 'utilities'
  | 'viewer'
  | 'channel'
  | 'chat'
  | 'emotes'
  | 'alerts'
  | 'rewards'
  | 'queue'
  | 'game-stats';

export interface VariableAvailability {
  readonly id: string;
  readonly group: LocaleText;
  readonly label: LocaleText;
  readonly dashPath: string;
}

export interface VariableExample {
  /** The concrete token a broadcaster can paste. */
  readonly syntax: string;
  /** The representative value shown by the builder for that token. */
  readonly output: string;
  readonly surfaceId: string;
}

export interface VariableLexerResult {
  readonly valid: boolean;
  readonly name?: string;
  readonly payload?: string | null;
  readonly reason?: string;
}

/** One canonical family in the public reference. */
export interface VariableReference {
  readonly id: string;
  /** The simplest concrete spelling, useful for a primary copy button. */
  readonly token: string;
  /** Canonical syntax. Parameterized families use readable placeholders. */
  readonly syntax: string;
  /** Primary concrete syntax used by simple reference cards/copy buttons. */
  readonly example: string;
  /** Representative output for the primary example. */
  readonly output: string;
  /** All accepted syntax shapes represented by the source surfaces. */
  readonly syntaxes: readonly string[];
  /** Concrete source examples, including distinct payload forms. */
  readonly examples: readonly VariableExample[];
  readonly name: LocaleText;
  readonly description: LocaleText;
  readonly category: VariableCategory;
  readonly categories: readonly VariableCategory[];
  /** Bare token names, without braces, for search and display. */
  readonly aliases: readonly string[];
  /** Alias spellings including braces, convenient for a copy/search UI. */
  readonly aliasTokens: readonly string[];
  /** Surface ids are stable and compact for URL filters. */
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly VariableAvailability[];
  /** Module/toggle hints found in the source descriptions. */
  readonly requirements: readonly string[];
  /** Convenience form for a compact card; `requirements` is authoritative. */
  readonly requirement: string;
  readonly payload: string;
  readonly behavior: string;
  readonly legacy: boolean;
  readonly parameterized: boolean;
  readonly lexer: VariableLexerResult;
  /** Kept as a direct boolean for simple consumers and tests. */
  readonly lexerValid: boolean;
}

export interface LocalizedVariableReference {
  readonly id: string;
  readonly token: string;
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
  readonly syntaxes: readonly string[];
  readonly examples: readonly { syntax: string; output: string; surfaceId: string }[];
  readonly name: string;
  readonly description: string;
  readonly category: VariableCategory;
  readonly categories: readonly VariableCategory[];
  readonly aliases: readonly string[];
  readonly aliasTokens: readonly string[];
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly { id: string; group: string; label: string; dashPath: string }[];
  readonly requirements: readonly string[];
  readonly requirement: string;
  readonly payload: string;
  readonly behavior: string;
  readonly legacy: boolean;
  readonly parameterized: boolean;
  readonly lexer: VariableLexerResult;
  readonly lexerValid: boolean;
}

const CATEGORY_ORDER: readonly VariableCategory[] = [
  'basics',
  'arguments',
  'counters',
  'dynamic',
  'utilities',
  'viewer',
  'channel',
  'chat',
  'emotes',
  'alerts',
  'rewards',
  'queue',
  'game-stats',
];

const CATEGORY_RANK = new Map(CATEGORY_ORDER.map((category, index) => [category, index]));

// Some names are aliases only in the custom-command message scope. `target`
// is also an actual field in clip/queue replies, so that context is kept as a
// separate family below rather than accidentally merging those meanings.
const CUSTOM_ALIASES: Readonly<Record<string, string>> = {
  sender: 'user',
  target: 'touser',
};

const POSITIONAL = /^\{(\d{1,2})\}$/;
const POSITIONAL_TAIL = /^\{(\d{1,2}):\}$/;
const ARGUMENT_FAMILY_RULES: readonly [RegExp, string][] = [
  [POSITIONAL, 'argument-word'],
  [POSITIONAL_TAIL, 'argument-tail'],
];
const PAYLOAD_FAMILY_IDS: Readonly<Record<string, readonly [string, string]>> = {
  counter: ['bound-counter', 'counter-increment'],
  count: ['queue-count', 'counter-read'],
};
const SONG_PARTS = new Set(['song.title', 'song.artist']);

const CUSTOM_CATEGORY_RULES: readonly [VariableCategory, RegExp][] = [
  ['arguments', /^\{(?:\d+|\d+:)\}$|^\{args\}$/],
  ['counters', /^\{(?:counter|count):|^\{uses\}$/],
  ['chat', /^\{(?:chatters|random\.chatter)/],
  ['emotes', /^\{(?:7tv|bttv|ffz|random\.emote)/],
  ['dynamic', /^\{(?:random|choice)/],
  ['utilities', /^\{(?:math|query|path|repeat|countdown|countup|if)/],
  ['viewer', /^\{(?:followage|accountage|points|pointsname|watchtime)/],
  ['channel', /^\{(?:channel|uptime|title|game|channel\.)/],
];

const ALERT_SURFACES = new Set(['follow', 'subscribe', 'cheer', 'raid']);
const CHAT_SURFACES = new Set(['shoutout', 'triggers', 'clip', 'time']);
const GAME_SURFACE_PREFIXES = ['bw-', 'mcsr-', 'fn-'];
const GAME_SURFACES = new Set(['bwstats', 'elo', 'sniper', 'tags']);

function pickLocale(text: LocaleText, lang: Lang): string {
  return text[lang as 'en' | 'fr'] ?? text.en;
}

function tokenParts(token: string): VarToken | null {
  const tokens = lex(token);
  if (tokens.length !== 1 || tokens[0].kind !== 'var') return null;
  return tokens[0];
}

/** Validate a complete concrete or placeholder syntax through the shared lexer. */
export function validateVariableSyntax(syntax: string): VariableLexerResult {
  const parsed = tokenParts(syntax);
  if (parsed === null) return { valid: false, reason: 'The syntax is not exactly one lexer variable token.' };
  return { valid: true, name: parsed.name, payload: parsed.payload };
}

/** Validate every syntax and example emitted by a reference record. */
export function validateVariableReference(reference: Pick<VariableReference, 'syntax' | 'syntaxes' | 'examples' | 'aliasTokens'>): boolean {
  return [reference.syntax, ...reference.syntaxes, ...reference.aliasTokens, ...reference.examples.map((example) => example.syntax)]
    .every((syntax) => validateVariableSyntax(syntax).valid);
}

function cleanRequirement(description: string): string[] {
  const requirements: string[] = [];
  // Descriptions intentionally use a stable, human-readable "Needs ..."
  // sentence. Pull only that sentence's subject so requirements remain short
  // chips rather than duplicating the whole explanation.
  const needs = /\bNeeds\s+(?:the\s+)?([^.!?]+?)(?:\s+module)?\./gi;
  for (const match of description.matchAll(needs)) {
    const value = match[1].trim().replace(/\s+/g, ' ');
    if (value && !requirements.includes(value)) requirements.push(value);
  }
  return requirements;
}

function categoryFor(surfaceId: string, token: string, hasPayload: boolean): VariableCategory {
  if (surfaceId === 'custom') return customCategoryFor(token);
  if (ALERT_SURFACES.has(surfaceId)) return 'alerts';
  if (surfaceId === 'channelpoints') return 'rewards';
  if (surfaceId.startsWith('queue-')) return 'queue';
  if (isGameSurface(surfaceId)) return 'game-stats';
  if (CHAT_SURFACES.has(surfaceId)) return 'chat';
  return hasPayload ? 'dynamic' : 'basics';
}

function customCategoryFor(token: string): VariableCategory {
  return CUSTOM_CATEGORY_RULES.find(([, rule]) => rule.test(token))?.[0] ?? 'basics';
}

function isGameSurface(surfaceId: string): boolean {
  return GAME_SURFACES.has(surfaceId) || GAME_SURFACE_PREFIXES.some((prefix) => surfaceId.startsWith(prefix));
}

function argumentFamilyId(token: string): string | undefined {
  return ARGUMENT_FAMILY_RULES.find(([rule]) => rule.test(token))?.[1];
}

function payloadFamilyId(name: string, payload: string | null): string | undefined {
  const families = PAYLOAD_FAMILY_IDS[name];
  if (families) return families[payload === null ? 0 : 1];
  return name === 'random' && payload !== null ? 'random' : undefined;
}

function familyId(surfaceId: string, token: string): string {
  const parsed = tokenParts(token);
  if (!parsed) return token;
  const name = parsed.name;
  if (surfaceId === 'custom' && name in CUSTOM_ALIASES) return CUSTOM_ALIASES[name];
  const argumentFamily = argumentFamilyId(token);
  if (argumentFamily) return argumentFamily;
  const payloadFamily = payloadFamilyId(name, parsed.payload);
  if (payloadFamily) return payloadFamily;
  // `{song.title}` and `{song.artist}` are documented as parts of the song
  // family even though the builder only needs the complete `{song}` chip.
  if (SONG_PARTS.has(name)) return 'song';
  return name;
}

const FAMILY_SYNTAX = new Map<string, string>([
  ['argument-word', '{1}'],
  ['argument-tail', '{1:}'],
  ['counter-increment', '{counter:name}'],
  ['counter-read', '{count:name}'],
  ['random', '{random:min-max}'],
  ['choice', '{choice:one,two,three}'],
  ['song', '{song}'],
]);

const VIEWER_PAYLOAD_FAMILIES = new Set([
  'followage', 'accountage', 'points', 'watchtime', 'quote', 'uptime', 'title', 'game',
]);

function familySyntax(id: string, firstToken: string): string {
  const syntax = FAMILY_SYNTAX.get(id);
  if (syntax) return syntax;
  return VIEWER_PAYLOAD_FAMILIES.has(id) ? `${firstToken.slice(0, -1)}:viewer}` : firstToken;
}

function familyParameterized(id: string, token: string): boolean {
  const parsed = tokenParts(token);
  return Boolean(parsed?.payload !== null) || id === 'argument-word' || id === 'argument-tail' || id === 'random' || id === 'choice';
}

interface MutableReference {
  id: string;
  token: string;
  syntax: string;
  example: string;
  output: string;
  syntaxes: string[];
  examples: VariableExample[];
  name: LocaleText;
  description: LocaleText;
  categories: VariableCategory[];
  aliases: string[];
  aliasTokens: string[];
  surfaceIds: string[];
  surfaces: VariableAvailability[];
  requirements: string[];
  requirement: string;
  payload: string;
  behavior: string;
  legacy: boolean;
  parameterized: boolean;
}

function addUnique<T>(items: T[], value: T): void {
  if (!items.includes(value)) items.push(value);
}

function sourceTokenForFamily(id: string, token: string): string {
  if (id === 'argument-word') return '{1}';
  if (id === 'argument-tail') return '{1:}';
  if (id === 'counter-increment') return '{counter:name}';
  if (id === 'counter-read') return '{count:name}';
  if (id === 'random') return '{random}';
  return token;
}

function isAliasForm(surfaceId: string, id: string, token: string): boolean {
  const parsed = tokenParts(token);
  if (!parsed || surfaceId !== 'custom') return false;
  return CUSTOM_ALIASES[parsed.name] === id;
}

function createReference(surface: typeof SURFACES[number], variable: typeof SURFACES[number]['vars'][number], id: string, category: VariableCategory, token: string, syntax: string): MutableReference {
  const requirements = cleanRequirement(variable.desc.en);
  const parameterized = familyParameterized(id, variable.token);
  return {
    id, token, syntax, example: variable.token, output: variable.sample,
    syntaxes: [syntax], examples: [{ syntax: variable.token, output: variable.sample, surfaceId: surface.id }],
    name: variable.name, description: variable.desc, categories: [category], aliases: [], aliasTokens: [],
    surfaceIds: [surface.id], surfaces: [{ id: surface.id, group: surface.group, label: surface.label, dashPath: surface.dashPath }],
    requirements, requirement: requirements.join(', '), payload: parameterized ? syntax : '',
    behavior: 'Unknown or disabled variables stay visible as written; an empty value can use a |fallback.',
    legacy: false, parameterized,
  };
}

function mergeReference(reference: MutableReference, surface: typeof SURFACES[number], variable: typeof SURFACES[number]['vars'][number], syntax: string, category: VariableCategory): void {
  addUnique(reference.syntaxes, syntax);
  if (!reference.examples.some((example) => example.syntax === variable.token && example.surfaceId === surface.id)) {
    reference.examples.push({ syntax: variable.token, output: variable.sample, surfaceId: surface.id });
  }
  addUnique(reference.categories, category);
  addUnique(reference.surfaceIds, surface.id);
  if (!reference.surfaces.some((item) => item.id === surface.id)) {
    reference.surfaces.push({ id: surface.id, group: surface.group, label: surface.label, dashPath: surface.dashPath });
  }
  for (const requirement of cleanRequirement(variable.desc.en)) addUnique(reference.requirements, requirement);
  reference.requirement = reference.requirements.join(', ');
  reference.parameterized ||= familyParameterized(reference.id, variable.token);
}

function addSurfaceVariable(byId: Map<string, MutableReference>, surface: typeof SURFACES[number], variable: typeof SURFACES[number]['vars'][number]): void {
  const parsed = tokenParts(variable.token);
  if (!parsed) return;
  const id = familyId(surface.id, variable.token);
  const category = categoryFor(surface.id, variable.token, variable.token.includes(':'));
  const token = sourceTokenForFamily(id, variable.token);
  const syntax = familySyntax(id, token);
  const reference = byId.get(id);
  if (reference) mergeReference(reference, surface, variable, syntax, category);
  else byId.set(id, createReference(surface, variable, id, category, token, syntax));
  if (isAliasForm(surface.id, id, variable.token)) {
    const target = byId.get(id)!;
    addUnique(target.aliases, parsed.name);
    addUnique(target.aliasTokens, variable.token);
  }
}

function addSupplementalForms(byId: Map<string, MutableReference>): void {
  const song = byId.get('song');
  if (song) for (const token of ['{song.title}', '{song.artist}']) addUnique(song.syntaxes, token);
  for (const [id, aliases] of Object.entries({ user: ['sender'], touser: ['target'] })) {
    const reference = byId.get(id);
    if (reference) for (const alias of aliases) {
      addUnique(reference.aliases, alias);
      addUnique(reference.aliasTokens, `{${alias}}`);
    }
  }
}

function finalizeReference(mutable: MutableReference): VariableReference {
  const categories = [...mutable.categories].sort((a, b) => (CATEGORY_RANK.get(a) ?? 99) - (CATEGORY_RANK.get(b) ?? 99));
  const aliases = mutable.aliases.filter((alias) => alias !== mutable.token.slice(1, -1));
  const aliasTokens = mutable.aliasTokens.filter((alias) => alias !== mutable.token);
  const lexerValid = validateVariableReference({ syntax: mutable.syntax, syntaxes: mutable.syntaxes, examples: mutable.examples, aliasTokens });
  const lexer = validateVariableSyntax(mutable.syntax);
  return { ...mutable, categories, category: categories[0], aliases, aliasTokens, lexer, lexerValid,
    syntaxes: [...mutable.syntaxes], examples: [...mutable.examples], surfaceIds: [...mutable.surfaceIds],
    surfaces: [...mutable.surfaces], requirements: [...mutable.requirements] };
}

function buildCatalog(): VariableReference[] {
  const byId = new Map<string, MutableReference>();
  for (const surface of SURFACES) for (const variable of surface.vars) addSurfaceVariable(byId, surface, variable);
  addSupplementalForms(byId);
  return [...byId.values()].map(finalizeReference).sort((a, b) => {
    const category = (CATEGORY_RANK.get(a.category) ?? 99) - (CATEGORY_RANK.get(b.category) ?? 99);
    return category || a.id.localeCompare(b.id);
  });
}

/** Canonical, exhaustive family list. The array and its records are immutable. */
const BUILT_VARIABLES = buildCatalog();
if (!validateVariableCatalog(BUILT_VARIABLES)) {
  throw new Error('The variable reference contains syntax that the shared lexer would parse differently.');
}
export const VARIABLES: readonly VariableReference[] = Object.freeze(BUILT_VARIABLES.map((reference) => Object.freeze(reference)));

// Descriptive aliases make the contract pleasant to discover at call sites.
export const VARIABLE_CATALOG = VARIABLES;
export const VARIABLE_REFERENCE = VARIABLES;
export const catalog = VARIABLES;

/** Return the complete reference with copy localized for one marketing locale. */
export function variableReferenceData(lang: Lang): readonly LocalizedVariableReference[] {
  return VARIABLES.map((reference) => ({
    ...reference,
    name: pickLocale(reference.name, lang),
    description: pickLocale(reference.description, lang),
    // Requirement chips are derived from the same localized description that
    // the visitor reads. The manifest keeps the stable family identifiers;
    // this avoids showing an English module requirement on the French page.
    requirements: cleanRequirement(pickLocale(reference.description, lang)),
    surfaces: reference.surfaces.map((surface) => ({
      id: surface.id,
      group: pickLocale(surface.group, lang),
      label: pickLocale(surface.label, lang),
      dashPath: surface.dashPath,
    })),
  }));
}

/** Search-friendly haystack for client-side filtering. */
export function variableSearchText(reference: VariableReference, lang: Lang = 'en'): string {
  const localized = variableReferenceData(lang).find((item) => item.id === reference.id);
  if (!localized) return '';
  return [
    localized.id,
    localized.token,
    ...localized.syntaxes,
    ...localized.aliases,
    ...localized.aliasTokens,
    localized.name,
    localized.description,
    ...localized.requirements,
    ...localized.categories,
    ...localized.surfaces.flatMap((surface) => [surface.id, surface.group, surface.label]),
  ].join(' ').toLocaleLowerCase();
}

/** All records currently shipped by the catalog must remain lexer-safe. */
export function validateVariableCatalog(records: readonly VariableReference[] = VARIABLES): boolean {
  return records.every((reference) => reference.lexerValid && validateVariableReference(reference));
}
