// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Live run snapshots from /deploys/<id>/stream.
//
// Every frame is the whole Run, so there is no merge: the newer seq replaces
// what is held. The seq check is what makes that safe, because two sources
// feed this object (the SSE frames and a load re-run after a form action)
// and they can land out of order; an older snapshot arriving second would
// otherwise roll the bars back.
//
// EventSource retries on its own after a dropped connection, but not after
// an HTTP error status (a 5xx while the console restarts, a 502 from the
// proxy): the spec leaves it CLOSED. That case is reopened here after a fixed
// delay so the page never silently stops moving.

import type { DeployRun } from '$lib/deploys/types';

export type StreamConn = 'connecting' | 'live' | 'reconnecting';

const REOPEN_MS = 3000;

export class RunStream {
  run = $state<DeployRun>() as DeployRun;
  conn = $state<StreamConn>('connecting');

  constructor(initial: DeployRun) {
    this.run = initial;
  }

  /** Take `next` when it is a different run or a newer snapshot of this one. */
  accept(next: DeployRun): void {
    if (next.id !== this.run.id || next.seq > this.run.seq) this.run = next;
  }

  private receive(e: MessageEvent<string>): void {
    try {
      this.accept(JSON.parse(e.data) as DeployRun);
    } catch {
      /* a malformed frame is dropped; the next one carries the full state */
    }
  }

  /** Opens the stream; the returned function closes it for good. */
  connect(url: string): () => void {
    let es: EventSource | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    const open = () => {
      es = new EventSource(url);
      es.onopen = () => (this.conn = 'live');
      es.addEventListener('run', (e) => this.receive(e as MessageEvent<string>));
      es.onerror = () => {
        if (this.conn === 'live') this.conn = 'reconnecting';
        if (es?.readyState !== EventSource.CLOSED || stopped) return;
        timer = setTimeout(open, REOPEN_MS);
      };
    };
    open();
    return () => {
      stopped = true;
      clearTimeout(timer);
      es?.close();
    };
  }
}
