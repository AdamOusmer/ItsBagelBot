// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DeployRun } from '$lib/deploys/types';

export type StreamConn = 'connecting' | 'live' | 'reconnecting';

const REOPEN_MS = 3000;

export class RunStream {
  run = $state<DeployRun>() as DeployRun;
  conn = $state<StreamConn>('connecting');

  constructor(initial: DeployRun) {
    this.run = initial;
  }

  accept(next: DeployRun): void {
    if (next.id !== this.run.id || next.seq > this.run.seq) this.run = next;
  }

  private receive(e: MessageEvent<string>): void {
    try {
      this.accept(JSON.parse(e.data) as DeployRun);
    } catch {
    }
  }

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
