// Minimal ambient types for the New Relic Node agent. The package ships no .d.ts
// and there is no maintained @types/newrelic for v12, so we declare only the
// surface we call. The agent itself is `external` (see vite.config) and resolved
// at runtime from the singleton preloaded via `--import newrelic`.
declare module 'newrelic' {
  type AttrValue = string | number | boolean;

  interface NewRelicApi {
    setTransactionName(name: string): void;
    addCustomAttributes(atts: Record<string, AttrValue>): void;
    setUserID(userID: string): void;
    noticeError(error: Error, customAttributes?: Record<string, AttrValue>): void;
    startSegment<T>(name: string, record: boolean, handler: () => T, callback?: () => void): T;
    getBrowserTimingHeader(options?: { nonce?: string; hasToRemoveScriptWrapper?: boolean }): string;
    getLinkingMetadata(omitSupportability?: boolean): Record<string, string>;
  }

  const api: NewRelicApi;
  export default api;
}
