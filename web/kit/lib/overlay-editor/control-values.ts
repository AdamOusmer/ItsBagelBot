// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SettingField } from './fields';

const MINUTE_MS = 60_000;
const MIB = 1024 * 1024;
const KIB = 1024;

export function dateToInput(value: string): string {
  const at = new Date(value);
  if (!Number.isFinite(at.valueOf())) return '';
  return new Date(at.valueOf() - at.getTimezoneOffset() * MINUTE_MS).toISOString().slice(0, 16);
}

export function inputToDate(value: string): string {
  return value ? new Date(value).toISOString() : '';
}

export function boundedNumber(field: SettingField, value: number): number | undefined {
  if (!Number.isFinite(value)) return undefined;
  return Math.min(field.max ?? Infinity, Math.max(field.min ?? -Infinity, value));
}

export function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string' && item !== '') : [];
}

export function refList(value: unknown, multiple: boolean): string[] {
  return multiple ? stringList(value) : stringList([value]);
}

export function formatBytes(bytes: number): string {
  if (bytes >= MIB) return `${(bytes / MIB).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / KIB))} KB`;
}

export function textLines(value: string): string[] {
  return value.split('\n').map((line) => line.trim()).filter(Boolean);
}
