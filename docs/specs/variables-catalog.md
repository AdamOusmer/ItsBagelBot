# Variables catalog: one manifest, one guide

Date: 2026-09-18
Status: Accepted. Implementation in stacked branches, phase 1 first.

## 1. Outcome

Every reply variable the bot expands is described once, in `web/kit/lib/variables/`, and every consumer derives from it: the dashboard token chips, the marketing command builder, the `/guides/variables` reference page and the rehearsal samples. A Go golden fixture binds that manifest to the resolver so a variable added on one side without the other fails CI.

Today one variable such as `{followage}` is hand-kept in six places (Go resolver, Go token catalog, kit rehearsal sample, marketing builder record with inline en/fr copy, dashboard chip list, kit locale hint) and module reply tokens in three (Go module maps, kit module catalog, marketing surfaces). Nothing checks they agree, and they do not: samples differ (`sesame_sam` vs `maya_live`), `{target}` and `{touser}` are each missing from one list, `{if}` exists only in the dashboard.

## 2. Vocabulary

See `CONTEXT.md`, section "Language: reply templates": Variable, Token, Form, Surface, Chip, Scope.

## 3. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | Go resolver owns behaviour; kit manifest owns the public inventory and copy; a golden fixture written by the Go test binds them. | Codegen from Go would put a Go build step in front of web CI and still leave locale copy elsewhere. A manifest with no parity test is how the drift happened. |
| D2 | Manifest home is `web/kit/lib/variables/`. | Kit is the shared data and engine home (`engine/tmpl`, `engine/rehearsal` already there). Dashboard cannot import marketing; marketing importing kit is the existing direction. |
| D3 | Copy lives in kit locales under `vars.<id>.{name,hint,desc,payload,behavior}`. Marketing reads it through `@bagel/kit/i18n`. | Inline `{en, fr}` objects bypass the FR-first i18n tooling and the literal-keys test. |
| D4 | Two copy lengths per variable: `hint` (one clause, chip tooltip) and `desc` (guide paragraph). | Truncating a paragraph gives garbage tooltips; the `{if}` description is the proof. |
| D5 | Surface-specific meaning stays on the module reply: `ModuleReply.tokens` becomes `{ name, sample, hintKey }[]` and `previewSamples` is deleted. | Same data, one shape, one sample value. |
| D6 | Parity is a golden JSON in `app/twitch/sesame/engine/scope/testdata/token_catalog.golden.json`, written by `go test -scope.write-golden`, read by a kit test. | Existing cross-language convention (`pure.golden.json`). Web CI has no Go toolchain, Go CI has no bun. |
| D7 | Guide page: grouped by category, left rail with counts, anchor per variable, collapsed rows that expand to a forms table, builder and dashboard links. Inline payload evaluation is a separate later PR. | The live evaluator pulls the rehearsal bundle onto the guide and needs its own size budget decision. |
| D8 | Deleted: marketing `catalog.ts` shim and its alias exports, `MutableVariableReference`, `COMMON_TOKEN_HEADS` and its test, the 53 `commandEditor.tok*` keys, the `*_SAMPLE` consts in `rehearsal.ts`, the per-family arrays in `builder.ts`. | Each is a second copy of something the manifest now owns. |

## 4. Manifest shape

```ts
interface VariableForm {
  syntax: string;   // '{followage:<login>}'
  example: string;  // '{followage:alex}'
  output: string;   // sample the rehearsal substitutes
}

interface VariableDef {
  id: string;                 // 'followage'; i18n key, guide anchor, golden id
  head: string;               // lexer head
  category: VariableCategory; // the existing 13-value union
  forms: VariableForm[];
  aliases?: string[];         // heads that resolve to the same value
  legacy?: boolean;
  requires?: string;          // module id the variable needs
}
```

Samples are one const each in `preview-values.ts`; `rehearsal.ts` and `forms[].output` both import them. `surfaces.ts` lists which variables each surface offers; module reply tokens come from the kit module catalog (`catalog/*.ts`).

## 5. Phases

1. `feat/kit-variables-manifest`: manifest, samples, locales, Go golden, parity test, rehearsal imports samples. Additive.
2. `feat/dashboard-chips-from-kit` (off 1): dashboard chips derive from the manifest; `ModuleReply.tokens` shape; delete `tok*` keys.
3. `feat/marketing-catalog-from-kit` (off 2): builder surfaces derive from kit; `lib/variables` shrinks to localize and group; delete shim and aliases.
4. `feat/variables-guide-redesign` (off 3): page, components, golden of the localized reference.
5. Later, separate decision: Go export of module reply token maps and a second golden.

Out of scope: Discord (no token resolver), importers (their target names are asserted to exist in the manifest, nothing else changes).
