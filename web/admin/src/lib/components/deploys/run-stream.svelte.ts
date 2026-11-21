// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DeployRun } from '#lib/deploys/types';

export type StreamConn = 'connecting' | 'live' | 'reconnecting';

const REOPEN_MS = 3000;

type RunConnection = {
  url: string;
  source?: EventSource;
  retry?: ReturnType<typeof setTimeout>;
};

export class RunStream {
  run = $state.raw<DeployRun>() as DeployRun;
  conn = $state<StreamConn>('connecting');

  constructor(initial: DeployRun) {
    this.run = initial;
  }

  accept(next: DeployRun): void {
    if (next.id === this.run.id && next.seq > this.run.seq) this.run = next;
  }

  private receive(e: MessageEvent<string>): void {
    try {
      this.accept(JSON.parse(e.data) as DeployRun);
    } catch {}
  }

  private open(connection: RunConnection): void {
    if (document.hidden) return;
    const source = new EventSource(connection.url);
    connection.source = source;
    source.onopen = () => {
      if (connection.source === source) this.conn = 'live';
    };
    source.addEventListener('run', (event) => {
      if (connection.source === source) this.receive(event as MessageEvent<string>);
    });
    source.onerror = () => {
      if (connection.source !== source || document.hidden) return;
      this.conn = 'reconnecting';
      if (source.readyState !== EventSource.CLOSED) return;
      connection.source = undefined;
      connection.retry = setTimeout(() => this.open(connection), REOPEN_MS);
    };
  }

  connect(url: string): () => void {
    const connection: RunConnection = { url };
    const close = () => {
      clearTimeout(connection.retry);
      connection.source?.close();
      connection.source = undefined;
      connection.retry = undefined;
    };
    const syncVisibility = () => {
      if (document.hidden) return close();
      if (connection.source) return;
      this.conn = 'connecting';
      this.open(connection);
    };

    document.addEventListener('visibilitychange', syncVisibility);
    syncVisibility();
    return () => {
      document.removeEventListener('visibilitychange', syncVisibility);
      close();
    };
  }
}
