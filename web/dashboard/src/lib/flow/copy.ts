// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { MAX_WAIT_SECONDS, type FieldSpec, type NodeSpec, type StepGroup, type VariableSpec } from './model';
import type { Translate } from './messages';

export const stepLabel = (t: Translate, spec: NodeSpec): string => t(`flows.step.${spec.kind}.${spec.slug}.label` as MessageKey);

export const stepSummary = (t: Translate, spec: NodeSpec): string => t(`flows.step.${spec.kind}.${spec.slug}.summary` as MessageKey);

export const groupLabel = (t: Translate, group: StepGroup): string => t(`flows.board.group.${group}` as MessageKey);

export const fieldLabel = (t: Translate, spec: NodeSpec, field: FieldSpec): string =>
  t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.label` as MessageKey);

export function fieldPlaceholder(t: Translate, spec: NodeSpec, field: FieldSpec): string | undefined {
  if (!('placeholder' in field) || !field.placeholder) return undefined;
  return t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.placeholder` as MessageKey);
}

export function fieldUnit(t: Translate, spec: NodeSpec, field: FieldSpec): string | undefined {
  if (field.type !== 'number' || !field.unit) return undefined;
  return t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.unit` as MessageKey, { max: MAX_WAIT_SECONDS });
}

export const optionText = (t: Translate, spec: NodeSpec, field: FieldSpec, value: string): string =>
  t(`flows.step.${spec.kind}.${spec.slug}.field.${field.key}.option.${value}` as MessageKey);

export function fieldOptions(t: Translate, spec: NodeSpec, field: FieldSpec): { value: string; label: string }[] {
  if (field.type !== 'select') return [];
  return field.options.map((value) => ({ value, label: optionText(t, spec, field, value) }));
}

export const outputLabel = (t: Translate, spec: NodeSpec, id: string): string =>
  spec.outputs ? t(`flows.step.${spec.kind}.${spec.slug}.out.${id}` as MessageKey) : t('flows.step.then');

export const variableLabel = (t: Translate, variable: VariableSpec): string => t(`flows.step.variable.${variable.name}` as MessageKey);
