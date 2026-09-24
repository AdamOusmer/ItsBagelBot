// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex, type VarToken } from '@bagel/kit/engine/tmpl';
import { VARIABLES as KIT_VARIABLES, type VariableDef, type VariableGroup } from '@bagel/kit/variables';
import { builtinDef } from '@bagel/kit/catalog/builtin-commands';
import { moduleDef } from '@bagel/kit/catalog';
import { SURFACES, kitText, type VarDef as SurfaceVarDef, type SurfaceDef } from '../../i18n/builder';
import type { Lang } from '../../i18n/lang';
import type {
  LocaleText,
  VariableAvailability,
  VariableExample,
  VariableReference,
  LocalizedVariableReference,
} from './types';
export type { LocaleText, VariableAvailability, VariableGroup, VariableExample, VariableReference, LocalizedVariableReference };

export const GROUP_ORDER: readonly VariableGroup[] = ['who', 'typed', 'stream', 'fun', 'data'];

const GROUP_RANK = new Map(GROUP_ORDER.map((group, index) => [group, index]));

function pickLocale(text: LocaleText, lang: Lang): string {
  return text[lang] ?? text.en;
}

function tokenParts(token: string): VarToken | null {
  const tokens = lex(token);
  if (tokens.length !== 1 || tokens[0].kind !== 'var') return null;
  return tokens[0];
}

const ALERT_SURFACES = new Set(['follow', 'subscribe', 'cheer', 'raid']);
const GAME_SURFACE_PREFIXES = ['bw-', 'mcsr-', 'fn-'];
const GAME_SURFACES = new Set(['bwstats', 'elo', 'sniper', 'tags']);

function isGameSurface(surfaceId: string): boolean {
  return GAME_SURFACES.has(surfaceId) || GAME_SURFACE_PREFIXES.some((prefix) => surfaceId.startsWith(prefix));
}

const STREAM_SURFACES = new Set(['clip', 'time']);

function surfaceOnlyGroup(surfaceId: string): VariableGroup {
  if (ALERT_SURFACES.has(surfaceId)) return 'stream';
  if (surfaceId === 'channelpoints') return 'data';
  if (surfaceId.startsWith('queue-')) return 'fun';
  if (isGameSurface(surfaceId)) return 'fun';
  if (STREAM_SURFACES.has(surfaceId)) return 'stream';
  return 'who';
}

function l10n(key: string): LocaleText {
  return { en: kitText('en', key), fr: kitText('fr', key) };
}

const EMPTY_LOCALE: LocaleText = Object.freeze({ en: '', fr: '' });

function kitRequirementIds(def: VariableDef): string[] {
  return def.requires ? [def.requires] : [];
}

function kitRequirement(def: VariableDef): string[] {
  if (!def.requires) return [];
  if (builtinDef(def.requires)) return [`!${def.requires}`];
  return [moduleDef(def.requires)?.label ?? def.requires];
}

function localizedRequirementLabel(id: string, lang: Lang, fallback: string): string {
  if (builtinDef(id)) return fallback;
  const key = `modules.catalog.${id}.label`;
  const text = kitText(lang, key);
  return text === key ? fallback : text;
}

interface Draft extends Omit<VariableReference, 'syntaxes' | 'examples' | 'groups' | 'aliases' | 'aliasTokens' | 'surfaceIds' | 'surfaces' | 'requirements'> {
  syntaxes: string[];
  examples: VariableExample[];
  groups: VariableGroup[];
  aliases: string[];
  aliasTokens: string[];
  surfaceIds: string[];
  surfaces: VariableAvailability[];
  requirementIds: string[];
  requirements: string[];
}

function kitRecord(def: VariableDef): Draft {
  const canonical = def.forms[0];
  const syntaxes = def.forms.map((form) => form.syntax);
  const examples = def.forms.map((form) => ({ syntax: form.example, output: form.output, surfaceId: 'custom' }));
  const aliases = [...(def.aliases ?? [])];
  const aliasTokens = aliases.map((alias) => `{${alias}}`);
  const requirements = kitRequirement(def);
  const requirementIds = kitRequirementIds(def);
  return {
    id: def.id, token: canonical.example, syntax: canonical.syntax, example: canonical.example, output: canonical.output,
    syntaxes, examples, name: l10n(`vars.${def.id}.name`), hint: l10n(`vars.${def.id}.hint`), description: l10n(`vars.${def.id}.desc`),
    group: def.group, groups: [def.group], aliases, aliasTokens,
    surfaceIds: [], surfaces: [], requirementIds, requirements, requirement: requirements.join(', '),
    legacy: def.legacy ?? false,
  };
}

function surfaceOnlyRecord(name: string, varCopy: SurfaceVarDef, surfaceId: string): Draft {
  const { token, sample } = varCopy;
  const syntax = `{${name}}`;
  const group = surfaceOnlyGroup(surfaceId);
  return {
    id: name, token, syntax, example: token, output: sample,
    syntaxes: [syntax], examples: [{ syntax: token, output: sample, surfaceId }],
    name: varCopy.name, hint: EMPTY_LOCALE, description: varCopy.desc, group, groups: [group],
    aliases: [], aliasTokens: [], surfaceIds: [], surfaces: [], requirementIds: [], requirements: [], requirement: '',
    legacy: false,
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
  const groups = [...draft.groups].sort((a, b) => (GROUP_RANK.get(a) ?? 99) - (GROUP_RANK.get(b) ?? 99));
  return { ...draft, group: groups[0], groups };
}

function buildCatalog(): VariableReference[] {
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
    const group = (GROUP_RANK.get(a.group) ?? 99) - (GROUP_RANK.get(b.group) ?? 99);
    return group || a.id.localeCompare(b.id);
  });
}

const BUILT_VARIABLES = buildCatalog();
export const VARIABLES: readonly VariableReference[] = Object.freeze(BUILT_VARIABLES.map((reference) => Object.freeze(reference)));

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
    ...localized.groups,
    ...localized.surfaces.flatMap((surface) => [surface.id, surface.group, surface.label]),
  ].join(' ').toLocaleLowerCase();
}
