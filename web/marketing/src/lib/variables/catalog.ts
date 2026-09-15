// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Stable module path for the guide/reference. Keep the implementation in
// index.ts so callers may also import the directory (`lib/variables`).
export {
  VARIABLES,
  VARIABLES as VARIABLE_CATALOG,
  VARIABLES as VARIABLE_REFERENCE,
  VARIABLES as catalog,
  variableReferenceData,
  variableSearchText,
  validateVariableCatalog,
  validateVariableReference,
  validateVariableSyntax,
} from './index';
export type {
  LocaleText,
  VariableAvailability,
  VariableCategory,
  VariableExample,
  VariableLexerResult,
  VariableReference,
  LocalizedVariableReference,
} from './index';
