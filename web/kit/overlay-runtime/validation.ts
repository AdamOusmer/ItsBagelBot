// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SettingField } from './fields';
import type { CustomField, CustomValue } from './widget-types';

export type JSONObject = Record<string, unknown>;

function recordValue(value: unknown): value is JSONObject {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export function object(value: unknown): JSONObject {
  if (!recordValue(value)) throw new Error('Expected an object.');
  return value;
}

const inRange = (value: number, min: number, max: number): boolean => value >= min && value <= max;

export function numberIn(value: unknown, min: number, max: number): number {
  if (typeof value !== 'number' || !inRange(value, min, max)) throw new Error(`Expected a number from ${min} to ${max}.`);
  return value;
}

export function textIn(value: unknown, max: number, min = 0): string {
  if (typeof value !== 'string' || !inRange(value.length, min, max)) throw new Error('Invalid text length.');
  return value;
}

export function flag(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new Error('Expected true or false.');
  return value;
}

export function array(value: unknown, max: number): unknown[] {
  if (!Array.isArray(value) || value.length > max) throw new Error('Invalid list.');
  return value;
}

export function choiceIn<T extends string>(value: unknown, options: readonly T[]): T {
  const result = textIn(value, 120) as T;
  if (!options.includes(result)) throw new Error(`Unknown option: ${result}`);
  return result;
}

export function validID(value: unknown): string {
  const id = textIn(value, 96, 1);
  if (!/^[a-zA-Z0-9_-]+$/.test(id)) throw new Error('Invalid widget ID.');
  return id;
}

const assetRef = /^asset:[a-zA-Z0-9_-]{1,96}$/;

function mediaRef(value: unknown): string {
  const ref = textIn(value, 110);
  if (ref && !assetRef.test(ref)) throw new Error('Expected an asset reference.');
  return ref;
}

function dateValue(value: unknown): string {
  const date = textIn(value, 64);
  if (date && !Number.isFinite(Date.parse(date))) throw new Error('Invalid date and time.');
  return date;
}

function colorValue(value: unknown): string {
  const color = textIn(value, 7);
  if (!/^#[0-9a-f]{6}$/i.test(color)) throw new Error('Use a hex color.');
  return color;
}

const checks: Record<SettingField['type'], (value: unknown, field: SettingField) => unknown> = {
  number: (value, f) => numberIn(value, f.min ?? 0, f.max ?? Number.MAX_SAFE_INTEGER),
  boolean: flag,
  select: (value, f) => choiceIn(value, f.options ?? []),
  media: mediaRef,
  'media-list': (value) => array(value, 100).map(mediaRef),
  list: (value) => array(value, 500).map((item) => textIn(item, 100)),
  text: (value) => textIn(value, 4000),
  family: (value) => textIn(value, 120),
  datetime: dateValue,
  color: colorValue
};

export function readFields(value: JSONObject, fields: SettingField[], defaults: JSONObject = {}): JSONObject {
  return Object.fromEntries(
    fields.flatMap((field) => {
      const raw = value[field.key] ?? defaults[field.key];
      if (raw === undefined) return [];
      return [[field.key, checks[field.type](raw, field)]];
    })
  );
}

const customTypes = ['text', 'number', 'boolean', 'color', 'select'] as const;

export function customValue(field: Pick<SettingField, 'type' | 'options'>, raw: unknown): CustomValue {
  if (field.type === 'number') return numberIn(raw, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER);
  return checks[field.type](raw, { key: '', ...field }) as CustomValue;
}

function readCustomField(input: unknown): CustomField {
  const value = object(input);
  const type = choiceIn(value.type, customTypes);
  const options = type === 'select' ? array(value.options, 100).map((option) => textIn(option, 100, 1)) : undefined;
  const field = { key: validID(value.key), label: textIn(value.label, 100, 1), type, options };
  return { ...field, default: customValue(field, value.default) };
}

export function customFields(value: unknown): CustomField[] {
  const fields = array(value, 100).map(readCustomField);
  if (new Set(fields.map((field) => field.key)).size !== fields.length) throw new Error('Custom field keys must be unique.');
  return fields;
}
