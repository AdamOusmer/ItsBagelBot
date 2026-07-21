type Expectation = {
  origin?: string;
  callbackPath?: string;
};

function parseURL(name: string, value: string | undefined): URL {
  if (!value) throw new Error(`${name} is not set`);
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`${name} must be an absolute URL`);
  }
  if (parsed.protocol !== 'https:' && parsed.hostname !== 'localhost') throw new Error(`${name} must use https`);
  return parsed;
}

export function assertOrigin(name: string, value: string | undefined): string {
  const parsed = parseURL(name, value);
  if (parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an origin only, without path/query/hash`);
  }
  return parsed.origin;
}

export function assertCallback(name: string, value: string | undefined, expected: Expectation): URL {
  const parsed = parseURL(name, value);
  if (expected.origin && parsed.origin !== expected.origin) {
    throw new Error(`${name} origin ${parsed.origin} does not match ${expected.origin}`);
  }
  if (expected.callbackPath && parsed.pathname !== expected.callbackPath) {
    throw new Error(`${name} path ${parsed.pathname} does not match ${expected.callbackPath}`);
  }
  if (parsed.search || parsed.hash) throw new Error(`${name} must not include query/hash`);
  return parsed;
}

/** Assert an optional setting is an absolute https URL when present. Unlike
 *  assertOrigin/assertCallback it allows a path (checkout pages), has no
 *  localhost escape hatch, and treats absence as fine. */
export function assertOptionalHTTPSURL(name: string, value: string | undefined): void {
  if (!value) return;
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`${name} must be an absolute URL`);
  }
  if (parsed.protocol !== 'https:') throw new Error(`${name} must use https`);
}

/** Read an optional positive-integer setting, falling back only when it is
 *  absent. Reject malformed values at boot instead of silently disabling a
 *  bound or accepting a capacity that JavaScript cannot represent exactly. */
export function positiveIntegerSetting(
  name: string,
  value: string | undefined,
  fallback: number
): number {
  if (value === undefined) return fallback;
  if (!/^[1-9]\d*$/.test(value)) throw new Error(`${name} must be a positive integer`);

  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) throw new Error(`${name} must be a safe integer`);
  return parsed;
}
