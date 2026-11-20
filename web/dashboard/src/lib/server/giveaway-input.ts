// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS as pins, giveawayRuleNumbers, giveawayRuleToggles, MOD, moduleDef } from '@bagel/kit';
import {
  giveawayDefaults, type GiveawayConfig, type GiveawayPreset, type GiveawayRules
} from '#lib/channel-giveaway';
import type { DrawInput, DrawMutation } from './channel-giveaways';

const namePattern = /^[a-z0-9-]+$/;
const uuidPattern = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/;
const roles = new Set(['everyone', 'sub', 'vip']);

const configFields = (moduleDef(MOD.giveaway)?.settings ?? []).filter((field) => !field.hidden);
const allowedFields = new Map(configFields.map((field) => [field.key, field]));
const toggleFormText = new Map([['true', 'on'], ['false', 'off']]);
const roundOnlyMutations = new Set<DrawMutation>(['close', 'extend', 'cancel']);
const slotRequiredMutations = new Set<DrawMutation>(['mark_delivered', 'skip_rule']);
const deliveredValues = new Map([['true', true], ['false', false]]);

const numberSettingValidators = new Map<string, (value: number) => boolean>([
  ['winners', (value) => integer(value, 1, pins.maxWinners)],
  ['windowSeconds', (value) => integer(value, pins.giveawayMinEntryWindow, pins.maxOpen)],
  ['claimSeconds', (value) => value === 0 || integer(value, pins.claimWindowMin, pins.claimWindowMax)]
]);

export function isPresetName(name: string): boolean {
  return name.length <= pins.giveawayPresetNameMax && namePattern.test(name);
}

function text(form: FormData, key: string): string {
  const value = form.get(key);
  return typeof value === 'string' ? value.trim() : '';
}

function integer(value: unknown, min: number, max: number): value is number {
  if (typeof value !== 'number' || !Number.isInteger(value)) return false;
  return value >= min && value <= max;
}

function parseObject(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== 'object') return null;
  return Array.isArray(value) ? null : value as Record<string, unknown>;
}

function validNumericRule(value: unknown, rule: (typeof giveawayRuleNumbers)[number]): boolean {
  if (!integer(value, 0, rule.max)) return false;
  if (rule.key !== 'sub_luck_percent') return true;
  return value === 0 || value >= pins.luckMin;
}

const ruleValidators = new Map<string, (value: unknown) => boolean>([
  ['role', (value) => typeof value === 'string' && roles.has(value)],
  ...giveawayRuleToggles.map((key) => [key, (value: unknown) => typeof value === 'boolean'] as const),
  ...giveawayRuleNumbers.map((rule) => [rule.key, (value: unknown) => validNumericRule(value, rule)] as const)
]);

function validRule([key, value]: [string, unknown]): boolean {
  return ruleValidators.get(key)?.(value) ?? false;
}

function readRules(value: unknown): GiveawayRules | null {
  const rules = parseObject(value);
  return rules && Object.entries(rules).every(validRule) ? rules as GiveawayRules : null;
}

function readPreset(value: unknown): GiveawayPreset | null {
  const obj = parseObject(value);
  if (typeof obj?.name !== 'string') return null;
  const name = obj.name.trim().toLowerCase();
  const rules = readRules(obj.rules ?? {});
  if (!isPresetName(name) || !rules) return null;
  return { name, rules };
}

function readPresets(value: unknown): GiveawayPreset[] | null {
  if (!Array.isArray(value) || value.length > pins.giveawayMaxPresets) return null;
  const presets = value.map(readPreset).filter((preset) => preset !== null);
  if (presets.length !== value.length) return null;
  return new Set(presets.map((preset) => preset.name)).size === presets.length ? presets : null;
}

function stringSetting(value: string, toggle: boolean): string {
  if (!toggle) return value;
  return toggleFormText.get(value) ?? value;
}

function formSetting(value: unknown, toggle: boolean): string | null {
  if (typeof value === 'string') return stringSetting(value, toggle);
  if (typeof value === 'boolean') return stringSetting(String(value), true);
  return typeof value === 'number' ? String(value) : null;
}

function readFields(config: Record<string, unknown>): GiveawayConfig['fields'] {
  const fields = { ...giveawayDefaults.fields };
  for (const { key, type } of configFields) fields[key] = formSetting(config[key], type === 'toggle') ?? fields[key] ?? '';
  return fields;
}

function hasPreset(presets: GiveawayPreset[], name: string): boolean {
  return presets.some((preset) => preset.name === name);
}

export function readGiveawayConfig(raw: unknown): GiveawayConfig {
  const config = parseObject(raw) ?? {};
  const fields = readFields(config);
  const presets = readPresets(config.presets) ?? [];
  const defaultPreset = String(config.defaultPreset ?? '').trim().toLowerCase();
  return { fields, presets, defaultPreset: hasPreset(presets, defaultPreset) ? defaultPreset : '' };
}

export function presetPatch(form: FormData): Pick<GiveawayConfig, 'presets' | 'defaultPreset'> | null {
  try {
    const presets = readPresets(JSON.parse(text(form, 'presets')));
    const defaultPreset = text(form, 'default_preset').toLowerCase();
    if (!presets) return null;
    if (defaultPreset && !hasPreset(presets, defaultPreset)) return null;
    return { presets, defaultPreset };
  } catch {
    return null;
  }
}

function validNumberSetting(key: string, value: string): boolean {
  if (value === '') return true;
  return numberSettingValidators.get(key)?.(Number(value)) ?? false;
}

function fieldValue(field: (typeof configFields)[number], value: string): string | number | null {
  switch (field.type) {
    case 'toggle':
      return value === 'on' || value === 'off' ? value : null;
    case 'number':
      return validNumberSetting(field.key, value) ? Number(value || giveawayDefaults.fields[field.key]) : null;
    default:
      return field.key === 'defaultPrize' && [...value].length > pins.giveawayPrizeMaxLen ? null : value;
  }
}

export function settingPatch(form: FormData): Record<string, string | number> | null {
  const field = allowedFields.get(text(form, 'key'));
  if (!field) return null;
  const value = fieldValue(field, text(form, 'value'));
  return value === null ? null : { [field.key]: value };
}

function startInput(form: FormData): DrawInput | null {
  const preset = text(form, 'preset').toLowerCase();
  const prize = text(form, 'prize');
  const winners = Number(text(form, 'winners'));
  const window_seconds = Number(text(form, 'window_seconds'));
  if (preset && !isPresetName(preset)) return null;
  if ([...prize].length > pins.giveawayPrizeMaxLen) return null;
  if (!integer(winners, 0, pins.maxWinners) || !integer(window_seconds, 0, pins.maxOpen)) return null;
  return { preset, prize, winners, window_seconds };
}

function slotInput(verb: DrawMutation, form: FormData): DrawInput | null {
  const slot = Number(text(form, 'slot'));
  const min = slotRequiredMutations.has(verb) ? 1 : 0;
  if (!integer(slot, min, pins.maxWinners)) return null;
  if (verb !== 'mark_delivered') return { slot };
  const delivered = deliveredValues.get(text(form, 'delivered'));
  return delivered === undefined ? null : { slot, delivered };
}

function drawRoundID(form: FormData) {
  const id = text(form, 'round_id');
  return id.length === pins.giveawayRoundIDMaxLen && uuidPattern.test(id) ? id : null;
}

export function drawInput(verb: DrawMutation, form: FormData): DrawInput | null {
  if (verb === 'start') return startInput(form);
  const round_id = drawRoundID(form);
  if (!round_id) return null;
  if (roundOnlyMutations.has(verb)) return { round_id };
  const input = slotInput(verb, form);
  return input ? { round_id, ...input } : null;
}
