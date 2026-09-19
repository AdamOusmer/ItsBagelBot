// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Localized copy shared by the variable manifest and its consumers. */
export type LocaleText = Readonly<Record<string, string>>;

export type VariableCategory =
  | 'basics'
  | 'arguments'
  | 'counters'
  | 'dynamic'
  | 'utilities'
  | 'viewer'
  | 'channel'
  | 'chat'
  | 'emotes'
  | 'alerts'
  | 'rewards'
  | 'queue'
  | 'game-stats';

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

export interface VariableLexerResult {
  readonly valid: boolean;
  readonly name?: string;
  readonly payload?: string | null;
  readonly reason?: string;
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
  readonly description: LocaleText;
  readonly category: VariableCategory;
  readonly categories: readonly VariableCategory[];
  /** Bare token names, without braces, for search and display. */
  readonly aliases: readonly string[];
  /** Alias spellings including braces, convenient for a copy/search UI. */
  readonly aliasTokens: readonly string[];
  /** Surface ids are stable and compact for URL filters. */
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly VariableAvailability[];
  /** Module/toggle hints found in the source descriptions. */
  readonly requirements: readonly string[];
  /** Convenience form for a compact card; `requirements` is authoritative. */
  readonly requirement: string;
  readonly payload: string;
  readonly behavior: string;
  readonly legacy: boolean;
  readonly parameterized: boolean;
  readonly lexer: VariableLexerResult;
  /** Kept as a direct boolean for simple consumers and tests. */
  readonly lexerValid: boolean;
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
  readonly description: string;
  readonly category: VariableCategory;
  readonly categories: readonly VariableCategory[];
  readonly aliases: readonly string[];
  readonly aliasTokens: readonly string[];
  readonly surfaceIds: readonly string[];
  readonly surfaces: readonly { id: string; group: string; label: string; dashPath: string }[];
  readonly requirements: readonly string[];
  readonly requirement: string;
  readonly payload: string;
  readonly behavior: string;
  readonly legacy: boolean;
  readonly parameterized: boolean;
  readonly lexer: VariableLexerResult;
  readonly lexerValid: boolean;
}
