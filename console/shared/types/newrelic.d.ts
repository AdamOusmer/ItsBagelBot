// Minimal ambient types for the New Relic Node agent. The runtime package does
// not ship TypeScript declarations, so keep the surface used by both apps here.
declare module 'newrelic' {
  type AttrValue = string | number | boolean;

  interface NewRelicApi {
    setTransactionName(name: string): void;
    addCustomAttributes(attributes: Record<string, AttrValue>): void;
    setUserID(userID: string): void;
    noticeError(error: Error, customAttributes?: Record<string, AttrValue>): void;
    recordMetric(name: string, value: number): void;
    startSegment<T>(name: string, record: boolean, handler: () => T, callback?: () => void): T;
    getBrowserTimingHeader(options?: { nonce?: string; hasToRemoveScriptWrapper?: boolean }): string;
    getLinkingMetadata(omitSupportability?: boolean): Record<string, string>;
  }

  const api: NewRelicApi;
  export default api;
}
