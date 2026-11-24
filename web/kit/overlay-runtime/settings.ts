// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayWidget } from '../lib/overlay-document';
import { settings } from './defaults';
import { alertFields, alertLook, creditHeaders, listRule, tierWeights, typography, variationFields, widgetFields } from './fields';
import { alertKinds, type AlertKind } from './feed';
import { array, object, readFields, type JSONObject } from './validation';
import type { AlertVariation, Widget, WidgetKind, WidgetSettings } from './widget-types';

function optionalObject(value: unknown): JSONObject {
  return value === undefined ? {} : object(value);
}

function style(value: unknown, defaults?: JSONObject): JSONObject {
  return readFields(optionalObject(value), typography, defaults);
}

function variation(value: unknown): AlertVariation {
  const entry = object(value);
  const overrides = object(entry.overrides);
  const fields = readFields(entry, variationFields);
  return {
    condition: fields.condition,
    value: fields.value,
    overrides: { ...readFields(overrides, alertLook), style: style(overrides.style) }
  } as AlertVariation;
}

function alertRule(value: unknown, defaults: JSONObject): JSONObject {
  const entry = optionalObject(value);
  return {
    ...readFields(entry, alertFields, defaults),
    style: style(entry.style, object(defaults.style)),
    variations: array(entry.variations ?? [], 100).map(variation)
  };
}

function rules(value: unknown, defaults: Record<AlertKind, unknown>, alert: boolean): JSONObject {
  const incoming = optionalObject(value);
  return Object.fromEntries(
    alertKinds.map((kind) => {
      const fallback = object(defaults[kind]);
      const rule = alert ? alertRule(incoming[kind], fallback) : readFields(optionalObject(incoming[kind]), listRule, fallback);
      return [kind, rule];
    })
  );
}

function customValues(value: unknown): JSONObject {
  const entries = Object.entries(optionalObject(value)).filter(([, item]) => ['string', 'number', 'boolean'].includes(typeof item));
  return Object.fromEntries(entries);
}

const extras: Partial<Record<WidgetKind, (value: JSONObject) => JSONObject>> = {
  alertbox: (value) => ({ events: rules(value.events, settings.alertbox.events, true) }),
  eventlist: (value) => ({ rules: rules(value.rules, settings.eventlist.rules, false) }),
  goal: (value) => ({ weights: readFields(optionalObject(value.weights), tierWeights, settings.goal.weights) }),
  credits: (value) => ({ headers: readFields(optionalObject(value.headers), creditHeaders, settings.credits.headers) }),
  custom: (value) => ({ values: customValues(value.values) })
};

export function parseSettings(kind: WidgetKind, value: unknown): WidgetSettings[WidgetKind] {
  const incoming = object(value);
  const fallback = object(settings[kind]);
  const result = readFields(incoming, widgetFields[kind], fallback);
  if (fallback.style) result.style = style(incoming.style, object(fallback.style));
  return { ...result, ...extras[kind]?.(incoming) } as WidgetSettings[WidgetKind];
}

export function parseWidget(input: OverlayWidget): Widget {
  const { settings: raw, ...base } = input;
  return { ...base, settings: parseSettings(input.kind, raw ?? {}) } as Widget;
}
