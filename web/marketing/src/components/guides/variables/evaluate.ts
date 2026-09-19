// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "Try it" row on a parameterized variable's forms table
// (docs/specs/variables-catalog.md D7): re-run the exact rehearsal the
// command builder uses (@bagel/kit/engine/rehearsal) as the visitor edits the
// token, rather than freezing the table's one canned example. A utility
// token (math, repeat, queryescape, pathescape, choice, random, countdown,
// countup) computes for real through the chain's Pure/Util scopes; anything
// else resolves through the same scopes the table's static output came from,
// so this never substitutes a value of its own.
//
// Split out of reference.ts because rehearsal.ts is not small (it pulls the
// lexer, the sample scopes and the response-line splitter): kept as its own
// module so the cost of the feature is one entry in web/kit/scripts/size.ts
// rather than folded into reference.ts's own footprint.
//
// No sample map is passed in: rehearseCommand's own chain already carries
// the same canned samples the guide's static table was built from (math,
// repeat, queryescape, pathescape, choice, random, countdown, countup,
// counter and the conditional compute for real off it; a name no scope in
// the chain owns -- only {urlfetch:...} among this guide's parameterized
// entries, since its real value exists only at send time -- renders
// "unknown" once edited, same as the command builder's own live preview
// would show it, and the row's server-rendered sample stands until then).
import { rehearseCommand, type Seg } from '@bagel/kit/engine/rehearsal';

/** Appends one rehearsed segment: an unknown token stays literal and muted
 * (exactly what chat would show for a name nothing resolves), everything
 * else prints as plain computed text. */
function appendSegment(output: HTMLElement, seg: Seg): void {
    if (seg.kind === 'unknown') {
        const span = document.createElement('span');
        span.className = 'vref-tryit__unknown';
        span.textContent = seg.text;
        output.append(span);
        return;
    }
    output.append(seg.text);
}

/** Re-rehearses `source` as a custom-command response and redraws `output`. */
function renderResult(output: HTMLElement, source: string): void {
    output.textContent = '';
    const segments = rehearseCommand(source).flatMap((line) => line.segments);
    for (const seg of segments) appendSegment(output, seg);
}

function wireOne(input: HTMLInputElement): void {
    const output = input.closest('.vref-tryit')?.querySelector<HTMLElement>('[data-vref-result]');
    if (!output) return;
    input.addEventListener('input', () => renderResult(output, input.value));
}

/** Wires every "Try it" input under `root` to re-rehearse on each edit. The
 * server-rendered value (the table's sample output) stands until the first
 * edit, so the row works with no JS too. */
export function wireTryIt(root: ParentNode): void {
    for (const input of root.querySelectorAll<HTMLInputElement>('[data-vref-try]')) wireOne(input);
}
