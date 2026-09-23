// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The exhaustive, broadcaster-facing variable reference.
 *
 * Kit's manifest (`@bagel/kit/variables`) is the source of truth for the
 * custom-command variables: one record below per kit VariableDef, its forms
 * becoming syntaxes/examples, its aliases/requires carried straight through
 * (docs/specs/variables-catalog.md D2/D3, phase 3 section B). A second kind
 * of record covers surface-only tokens: module reply fields (`{tier}`,
 * `{bits}`, `{player}`...) that are not custom-command variables, grouped by
 * their bare token name the same way a broadcaster would recognize them
 * reused across module replies.
 *
 * This is deliberately not a resolver. An unknown `{anything}` is still a
 * perfectly valid token to the lexer and is left literal by the bot. Runtime
 * ownership belongs to the scopes; this catalog describes the public forms
 * that the marketing site promises and uses the shared lexer to check them.
 */

import { lex, type VarToken } from '@bagel/kit/engine/tmpl';
import { VARIABLES as KIT_VARIABLES, type VariableDef } from '@bagel/kit/variables';
import { builtinDef } from '@bagel/kit/catalog/builtin-commands';
import { moduleDef } from '@bagel/kit/catalog';
import { SURFACES, kitText, type VarDef as SurfaceVarDef, type SurfaceDef } from '../../i18n/builder';
// From lang.ts, not ui.ts: ui.ts's module body runs import.meta.glob, which
// bun test cannot evaluate (see lang.ts).
import type { Lang } from '../../i18n/lang';
import type {
  LocaleText,
  VariableAvailability,
  VariableCategory,
  VariableExample,
  VariableLexerResult,
  VariableReference,
  LocalizedVariableReference,
} from './types';
export type { LocaleText, VariableAvailability, VariableCategory, VariableExample, VariableLexerResult, VariableReference, LocalizedVariableReference };

/** Left-rail and sort order for the guide page; also the canonical grouping order used everywhere the catalog is walked by category. */
export const CATEGORY_ORDER: readonly VariableCategory[] = [
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

function pickLocale(text: LocaleText, lang: Lang): string {
  return text[lang] ?? text.en;
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

const ALERT_SURFACES = new Set(['follow', 'subscribe', 'cheer', 'raid']);
const GAME_SURFACE_PREFIXES = ['bw-', 'mcsr-', 'fn-'];
const GAME_SURFACES = new Set(['bwstats', 'elo', 'sniper', 'tags']);

function isGameSurface(surfaceId: string): boolean {
  return GAME_SURFACES.has(surfaceId) || GAME_SURFACE_PREFIXES.some((prefix) => surfaceId.startsWith(prefix));
}

/** Category for a token that is not one of kit's own variables (a module
 * reply field). Kit-backed records use the manifest's own category instead
 * (see kitRecord below); this only covers the surface-only remainder. */
function surfaceOnlyCategory(surfaceId: string): VariableCategory {
  if (ALERT_SURFACES.has(surfaceId)) return 'alerts';
  if (surfaceId === 'channelpoints') return 'rewards';
  if (surfaceId.startsWith('queue-')) return 'queue';
  if (isGameSurface(surfaceId)) return 'game-stats';
  return 'chat'; // shoutout, triggers, clip, time
}

function l10n(key: string): LocaleText {
  return { en: kitText('en', key), fr: kitText('fr', key) };
}

/** A kit key that only some variables carry (vars.<id>.payload, .behavior):
 * kitText hands back the key itself when a locale lacks it, and a guide line
 * reading "vars.if.behavior" is worse than no line. */
function optionalL10n(key: string): LocaleText {
  const text = l10n(key);
  return text.en === key ? EMPTY_LOCALE : text;
}

/** Surface-only records have no kit hint key; the guide falls back to the description's first sentence for these. */
const EMPTY_LOCALE: LocaleText = Object.freeze({ en: '', fr: '' });

function kitRequirementIds(def: VariableDef): string[] {
  return def.requires ? [def.requires] : [];
}

/** A requirement is a module (localized label below) or a toggleable
 * built-in command (followage, accountage, uptime, title, game), which has
 * no localized label anywhere: its chat trigger is the name broadcasters
 * know it by in every language, so the chip says "!followage". */
function kitRequirement(def: VariableDef): string[] {
  if (!def.requires) return [];
  if (builtinDef(def.requires)) return [`!${def.requires}`];
  return [moduleDef(def.requires)?.label ?? def.requires];
}

/**
 * A module label in the broadcaster's own language, when the kit locales
 * carry one for that module id (modules.catalog.<id>.label, the same string
 * the dashboard module tile shows). `translate()` returns the key
 * unchanged when nothing matches, which is how a miss is told apart from a
 * real (possibly identical) translation without a second lookup.
 *
 * Falls back to the catalog's English label otherwise -- ModuleDef.label is
 * a plain hardcoded string (web/kit/lib/catalog/module-def.ts), not a
 * LocaleText, so most module ids have no French spelling to reach for yet.
 */
function localizedRequirementLabel(id: string, lang: Lang, fallback: string): string {
  if (builtinDef(id)) return fallback;
  const key = `modules.catalog.${id}.label`;
  const text = kitText(lang, key);
  return text === key ? fallback : text;
}

/** Mutable while surfaces are attached below; frozen by finalizeReference. */
interface Draft extends Omit<VariableReference, 'syntaxes' | 'examples' | 'categories' | 'aliases' | 'aliasTokens' | 'surfaceIds' | 'surfaces' | 'requirements'> {
  syntaxes: string[];
  examples: VariableExample[];
  categories: VariableCategory[];
  aliases: string[];
  aliasTokens: string[];
  surfaceIds: string[];
  surfaces: VariableAvailability[];
  requirementIds: string[];
  requirements: string[];
}

/** One record per kit VariableDef: forms become syntaxes/examples, aliases
 * become aliases/aliasTokens, requires becomes requirements. */
function kitRecord(def: VariableDef): Draft {
  const canonical = def.forms[0];
  const parameterized = canonical.syntax !== canonical.example;
  const syntaxes = def.forms.map((form) => form.syntax);
  const examples = def.forms.map((form) => ({ syntax: form.example, output: form.output, surfaceId: 'custom' }));
  const aliases = [...(def.aliases ?? [])];
  const aliasTokens = aliases.map((alias) => `{${alias}}`);
  const requirements = kitRequirement(def);
  const requirementIds = kitRequirementIds(def);
  return {
    id: def.id, token: canonical.example, syntax: canonical.syntax, example: canonical.example, output: canonical.output,
    syntaxes, examples, name: l10n(`vars.${def.id}.name`), hint: l10n(`vars.${def.id}.hint`), description: l10n(`vars.${def.id}.desc`),
    category: def.category, categories: [def.category], aliases, aliasTokens,
    surfaceIds: [], surfaces: [], requirementIds, requirements, requirement: requirements.join(', '),
    payload: optionalL10n(`vars.${def.id}.payload`), behavior: optionalL10n(`vars.${def.id}.behavior`), legacy: def.legacy ?? false,
    parameterized, lexer: validateVariableSyntax(canonical.syntax),
    lexerValid: validateVariableReference({ syntax: canonical.syntax, syntaxes, examples, aliasTokens }),
  };
}

/** One record for a bare token name that is not any kit variable's head
 * (a module reply field like `{tier}` or `{bits}`). First surface to use a
 * name sets its copy; later surfaces sharing the same bare name (matching a
 * broadcaster's own read of "I've seen {player} before") just add their
 * membership, the same simplification the deleted per-surface arrays used. */
function surfaceOnlyRecord(name: string, varCopy: SurfaceVarDef, surfaceId: string): Draft {
  const { token, sample } = varCopy;
  const syntax = `{${name}}`;
  return {
    id: name, token, syntax, example: token, output: sample,
    syntaxes: [syntax], examples: [{ syntax: token, output: sample, surfaceId }],
    name: varCopy.name, hint: EMPTY_LOCALE, description: varCopy.desc, category: surfaceOnlyCategory(surfaceId), categories: [surfaceOnlyCategory(surfaceId)],
    aliases: [], aliasTokens: [], surfaceIds: [], surfaces: [], requirementIds: [], requirements: [], requirement: '',
    payload: EMPTY_LOCALE, behavior: EMPTY_LOCALE, legacy: false, parameterized: false,
    lexer: validateVariableSyntax(syntax), lexerValid: validateVariableReference({ syntax, syntaxes: [syntax], examples: [], aliasTokens: [] }),
  };
}

function attachSurface(record: Draft, surface: SurfaceDef): void {
  if (record.surfaceIds.includes(surface.id)) return;
  record.surfaceIds.push(surface.id);
  record.surfaces.push({ id: surface.id, group: surface.group, label: surface.label, dashPath: surface.dashPath });
}

function addExample(record: Draft, token: string, sample: string, surfaceId: string): void {
  if (record.examples.some((example) => example.syntax === token && example.surfaceId === surfaceId)) return;
  record.examples.push({ syntax: token, output: sample, surfaceId });
}

function finalizeReference(draft: Draft): VariableReference {
  const categories = [...draft.categories].sort((a, b) => (CATEGORY_RANK.get(a) ?? 99) - (CATEGORY_RANK.get(b) ?? 99));
  return { ...draft, category: categories[0], categories };
}

function buildCatalog(): VariableReference[] {
  // Match by head only, never by alias: kit's aliases (`sender`→user,
  // `target`→touser) describe the custom-command engine's own scope, but a
  // module reply's `{target}` (a clip title, the next queue player) means
  // something unrelated that happens to share the spelling. Matching by
  // head still correctly merges the shared dynamic tokens ({random},
  // {choice}), which really are the same variable on every surface.
  //
  // Head-matching is the FALLBACK, not the rule, because it breaks on a form
  // whose own lexer head disagrees with its variable's: positional's {:m}
  // form lexes to the empty name (not "positional"), so byHead.get('') found
  // nothing and minted a bogus record (id "", syntax "{}"). A VarDef built
  // from a kit VariableForm carries kitId (see builder.ts's kitVarDef) for
  // exactly this reason — it names its own owner directly, so this loop never
  // has to re-derive it from the form text. Only a hand-written module-reply
  // VarDef (no kit variable behind it) falls through to byHead.
  const byHead = new Map(KIT_VARIABLES.map((def) => [def.head, def]));
  const byId = new Map<string, Draft>(KIT_VARIABLES.map((def) => [def.id, kitRecord(def)]));

  for (const surface of SURFACES) {
    for (const variable of surface.vars) {
      const parsed = tokenParts(variable.token);
      if (!parsed) continue;
      const id = variable.kitId ?? byHead.get(parsed.name)?.id ?? parsed.name;
      let record = byId.get(id);
      if (!record) {
        record = surfaceOnlyRecord(id, variable, surface.id);
        byId.set(id, record);
      }
      attachSurface(record, surface);
      addExample(record, variable.token, variable.sample, surface.id);
    }
  }

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

/** Return the complete reference with copy localized for one marketing locale. */
export function variableReferenceData(lang: Lang): readonly LocalizedVariableReference[] {
  return VARIABLES.map((reference) => {
    const requirements = reference.requirements.map((fallback, index) =>
      localizedRequirementLabel(reference.requirementIds[index] ?? '', lang, fallback)
    );
    return {
      ...reference,
      name: pickLocale(reference.name, lang),
      hint: pickLocale(reference.hint, lang),
      description: pickLocale(reference.description, lang),
      payload: pickLocale(reference.payload, lang),
      behavior: pickLocale(reference.behavior, lang),
      surfaces: reference.surfaces.map((surface) => ({
        id: surface.id,
        group: pickLocale(surface.group, lang),
        label: pickLocale(surface.label, lang),
        dashPath: surface.dashPath,
      })),
      requirements,
      requirement: requirements.join(', '),
    };
  });
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
