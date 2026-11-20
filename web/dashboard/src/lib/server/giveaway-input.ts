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
const boolFields = new Set<string>(giveawayRuleToggles);

export function isPresetName(name: string): boolean {
  return name.length <= pins.giveawayPresetNameMax && namePattern.test(name);
}

function text(form: FormData, key: string): string {
  const value = form.get(key);
  return typeof value === 'string' ? value.trim() : '';
}

function integer(value: unknown, min: number, max: number): value is number {
  if (typeof value !== 'number') return false;
  return Number.isInteger(value) && value >= min && value <= max;
}

function parseObject(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function validLuck(value: number): boolean {
  return value === 0 || value >= pins.luckMin;
}

function validRule([key, value]: [string, unknown]): boolean {
  if (boolFields.has(key)) return typeof value === 'boolean';
  if (key === 'role') return typeof value === 'string' && roles.has(value);
  const pin = giveawayRuleNumbers.find((rule) => rule.key === key);
  if (!pin || !integer(value, 0, pin.max)) return false;
  return key !== 'sub_luck_percent' || validLuck(value);
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
  const freezeWhileOpen = obj.freezeWhileOpen;
  if (freezeWhileOpen == null) return { name, rules };
  return typeof freezeWhileOpen === 'boolean' ? { name, rules, freezeWhileOpen } : null;
}

function readPresets(value: unknown): GiveawayPreset[] | null {
  if (!Array.isArray(value) || value.length > pins.giveawayMaxPresets) return null;
  const presets = value.map(readPreset).filter((preset) => preset !== null);
  if (presets.length !== value.length) return null;
  return new Set(presets.map((preset) => preset.name)).size === presets.length ? presets : null;
}

function formSetting(value: unknown, toggle: boolean): string | null {
  if (typeof value === 'number') return String(value);
  if (toggle && (value === 'true' || value === 'false')) return value === 'true' ? 'on' : 'off';
  if (typeof value === 'string') return value;
  if (typeof value === 'boolean') return value ? 'on' : 'off';
  return null;
}

export function readGiveawayConfig(raw: unknown): GiveawayConfig {
  const config = parseObject(raw) ?? {};
  const fields = { ...giveawayDefaults.fields };
  for (const { key, type } of configFields) fields[key] = formSetting(config[key], type === 'toggle') ?? fields[key] ?? '';
  const presets = readPresets(config.presets) ?? [];
  const defaultPreset = String(config.defaultPreset ?? '').trim().toLowerCase();
  return { fields, presets, defaultPreset: presets.some((preset) => preset.name === defaultPreset) ? defaultPreset : '' };
}

export function presetPatch(form: FormData): Pick<GiveawayConfig, 'presets' | 'defaultPreset'> | null {
  try {
    const presets = readPresets(JSON.parse(text(form, 'presets')));
    const defaultPreset = text(form, 'default_preset').toLowerCase();
    if (!presets) return null;
    if (defaultPreset && !presets.some((preset) => preset.name === defaultPreset)) return null;
    return { presets, defaultPreset };
  } catch {
    return null;
  }
}

function validNumberSetting(key: string, value: string): boolean {
  if (value === '') return true;
  const number = Number(value);
  switch (key) {
    case 'winners': return integer(number, 1, pins.maxWinners);
    case 'windowSeconds': return integer(number, pins.giveawayMinEntryWindow, pins.maxOpen);
    case 'claimSeconds': return number === 0 || integer(number, pins.claimWindowMin, pins.claimWindowMax);
    default: return false;
  }
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
  const min = verb === 'mark_delivered' || verb === 'skip_rule' ? 1 : 0;
  if (!integer(slot, min, pins.maxWinners)) return null;
  if (verb !== 'mark_delivered') return { slot };
  const delivered = text(form, 'delivered');
  return delivered === 'true' || delivered === 'false' ? { slot, delivered: delivered === 'true' } : null;
}

export function drawInput(verb: DrawMutation, form: FormData): DrawInput | null {
  if (verb === 'start') return startInput(form);
  const round_id = text(form, 'round_id');
  if (!uuidPattern.test(round_id) || round_id.length > pins.giveawayRoundIDMaxLen) return null;
  switch (verb) {
    case 'close':
    case 'extend':
    case 'cancel':
      return { round_id };
    default: {
      const input = slotInput(verb, form);
      return input ? { round_id, ...input } : null;
    }
  }
}
