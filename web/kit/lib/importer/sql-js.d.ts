// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Minimal ambient module declaration for sql.js@1.14.2, which ships no TypeScript
// types. Only the surface streamlabsdesktop.ts uses is declared; the wasm build
// (dist/sql-wasm.js) is the Node/default entry per its exports map.
declare module 'sql.js' {
  export interface Database {
    run(sql: string, params?: unknown[]): void;
    exec(sql: string): QueryExecResult[];
    close(): void;
  }
  export interface QueryExecResult {
    columns: string[];
    values: unknown[][];
  }
  export interface SqlJsStatic {
    Database: new (data?: Uint8Array | Buffer | null) => Database;
  }
  export default function initSqlJs(config?: {
    locateFile?: (file: string) => string;
  }): Promise<SqlJsStatic>;
}
