// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { isSetupValue } from '@bagel/kit/automations';
import type { FlowNode } from './model';

export const awaitingSetup = (node: FlowNode): boolean => Object.values(node.config).some(isSetupValue);

export function configText(node: FlowNode, key: string): string {
  const value = node.config[key];
  return isSetupValue(value) ? '' : String(value ?? '');
}
