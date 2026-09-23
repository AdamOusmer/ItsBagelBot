// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// bump_counter command-option validation, split out of commands-validate.ts
// the same way fetch-validate.ts split the $(urlfetch) validators off that
// file: a small, standalone module rather than one more string-shaped
// function folded into an already string-heavy file.

/** Mirrors COMMAND_NAME_MAX (commands-validate.ts): a bump-counter name is
 * held to the same length rule because it is likewise echoed to chat,
 * through the {counter:…}/{count:…} reads of the value this option
 * produces. */
const COUNTER_NAME_MAX = 64;

// bumpCounterProblem mirrors validate.BumpCounter (Go): '' is valid (no
// bump), unlike a command name, which can never be empty; a NAME is held to
// the same length/charset rule because it is likewise echoed to chat, through
// the {counter:…}/{count:…} reads of the value this option produces.
//
// A leading '!' is stripped before checking anything else, matching Go's
// own order: CommandSpec.normalize folds bump_counter through
// tmpl.NormalizeName (trim, drop one leading '!', trim, lower-case) BEFORE
// validate.BumpCounter ever runs, so Go never sees a leading '!' to reject
// in the first place. This function used to reject '!' ANYWHERE, which was
// stricter than Go two ways at once: Go allows one embedded further in the
// name (nothing here strips or forbids that), and Go never even looks at a
// leading one because normalization already removed it. Stripping here
// makes this the same check regardless of whether a caller normalized
// first (both call sites currently do; a future one might not).
//
// ':' is rejected for the same reason Go's validate.BumpCounter now is: it
// is the {counter:…}/{count:…} token's payload separator (see that
// function's comment), so a name containing one could never be addressed
// by either token.
export function bumpCounterProblem(name: string): string | undefined {
  const stripped = name.replace(/^!/, '');
  if (!stripped) return undefined;
  if (stripped.length > COUNTER_NAME_MAX) return `Counter name must be at most ${COUNTER_NAME_MAX} characters.`;
  if (/\s/.test(stripped)) return 'Counter name cannot contain spaces.';
  if (stripped.includes(':')) return 'Counter name cannot contain ":".';
  return undefined;
}
