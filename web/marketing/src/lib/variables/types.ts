// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type { VariableGroup } from '@bagel/kit/variables';
import type { VariableGroup } from '@bagel/kit/variables';

export type LocaleText = Readonly<Record<string, string>>;

export interface VariableAvailability {
  readonly id: string;
  readonly group: LocaleText;
  readonly label: LocaleText;
  readonly dashPath: string;
}

export interface VariableExample {
  readonly syntax: string;
  readonly output: string;
  readonly surfaceId: string;
}

export interface VariableReference {
  readonly id: string;
  readonly token: string;
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
  readonly syntaxes: readonly string[];
  readonly examples: readonly VariableExample[];
  readonly name: LocaleText;
  readonly hint: LocaleText;
  readonly description: LocaleText;
  readonly group: VariableGroup;
  readonly groups: readonly VariableGroup[];
  readonly aliases: readonly string[];
  readonly aliasTokens: readonly string[];
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly VariableAvailability[];
  readonly requirementIds: readonly string[];
  readonly requirements: readonly string[];
  readonly requirement: string;
  readonly legacy: boolean;
}

export interface LocalizedVariableReference {
  readonly id: string;
  readonly token: string;
  readonly syntax: string;
  readonly example: string;
  readonly output: string;
  readonly syntaxes: readonly string[];
  readonly examples: readonly { syntax: string; output: string; surfaceId: string }[];
  readonly name: string;
  readonly hint: string;
  readonly description: string;
  readonly group: VariableGroup;
  readonly groups: readonly VariableGroup[];
  readonly aliases: readonly string[];
  readonly aliasTokens: readonly string[];
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly { id: string; group: string; label: string; dashPath: string }[];
  readonly requirementIds: readonly string[];
  readonly requirements: readonly string[];
  readonly requirement: string;
  readonly legacy: boolean;
}
