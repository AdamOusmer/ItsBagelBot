// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// VariableCategory used to be its own 13-value union here; it is now kit's
// VariableGroup (5 values: who/typed/stream/fun/data), re-exported so every
// import of the old name keeps working (docs/specs/variables-catalog.md
// phase 4).
export type { VariableGroup } from '@bagel/kit/variables';
import type { VariableGroup } from '@bagel/kit/variables';

/** Localized copy shared by the variable manifest and its consumers. */
export type LocaleText = Readonly<Record<string, string>>;

export interface VariableAvailability {
  readonly id: string;
  readonly group: LocaleText;
  readonly label: LocaleText;
  readonly dashPath: string;
}

export interface VariableExample {
  /** The concrete token a broadcaster can paste. */
  readonly syntax: string;
  /** The representative value shown by the builder for that token. */
  readonly output: string;
  readonly surfaceId: string;
}

/** One canonical family in the public reference. */
export interface VariableReference {
  readonly id: string;
  /** The simplest concrete spelling, useful for a primary copy button. */
  readonly token: string;
  /** Canonical syntax. Parameterized families use readable placeholders. */
  readonly syntax: string;
  /** Primary concrete syntax used by simple reference cards/copy buttons. */
  readonly example: string;
  /** Representative output for the primary example. */
  readonly output: string;
  /** All accepted syntax shapes represented by the source surfaces. */
  readonly syntaxes: readonly string[];
  /** Concrete source examples, including distinct payload forms. */
  readonly examples: readonly VariableExample[];
  readonly name: LocaleText;
  /** One-line summary for a collapsed guide row; empty when the source kit variable has none, which the guide falls back from to the description's first sentence. */
  readonly hint: LocaleText;
  readonly description: LocaleText;
  readonly group: VariableGroup;
  readonly groups: readonly VariableGroup[];
  /** Bare token names, without braces, for search and display. */
  readonly aliases: readonly string[];
  /** Alias spellings including braces, convenient for a copy/search UI. */
  readonly aliasTokens: readonly string[];
  /** Surface ids are stable and compact for URL filters. */
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly VariableAvailability[];
  /** Module ids a requirement label was resolved from, parallel to `requirements`; empty entries have no owning module id. Lets a locale-aware consumer look up its own label per module instead of trusting the English one baked in at catalog-build time. */
  readonly requirementIds: readonly string[];
  /** Module/toggle hints found in the source descriptions. */
  readonly requirements: readonly string[];
  /** Convenience form for a compact card; `requirements` is authoritative. */
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
