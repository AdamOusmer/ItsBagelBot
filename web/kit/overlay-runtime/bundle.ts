// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { customFields, customValue, object, textIn } from './validation';
import type { CustomField, CustomValue } from './widget-types';

export const MAX_BUNDLE_BYTES = 512 * 1024;

export type WidgetBundle = { html: string; css: string; js: string; fields: CustomField[] };

export function parseBundle(text: string): WidgetBundle {
  if (new TextEncoder().encode(text).length > MAX_BUNDLE_BYTES) throw new Error('Widget bundle is too large.');
  const value = object(JSON.parse(text));
  return {
    html: textIn(value.html, MAX_BUNDLE_BYTES),
    css: textIn(value.css, MAX_BUNDLE_BYTES),
    js: textIn(value.js, MAX_BUNDLE_BYTES),
    fields: customFields(value.fields ?? [])
  };
}

export function fieldValues(fields: CustomField[], values: Record<string, CustomValue>): Record<string, CustomValue> {
  return Object.fromEntries(
    fields.map((field) => {
      const supplied = Object.hasOwn(values, field.key) ? values[field.key] : undefined;
      return [field.key, customValue(field, supplied ?? field.default)];
    })
  );
}
