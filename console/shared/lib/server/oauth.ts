// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Twitch OAuth2/OIDC client built on oauth4webapi (Panva's maintained protocol
// library — the successor ecosystem to the deprecated `arctic` package this
// used to be). This module is only arg-marshaling: every protocol-sensitive
// step — client_secret_post encoding, the token request itself, and ID Token
// claim validation (iss, aud, exp, nonce) — happens inside oauth4webapi.
//
// Twitch metadata is pinned rather than discovered: discovery would add a
// network round trip and failure mode to every cold login for three endpoints
// that have been stable for years. Same trust model as arctic: the id_token
// claims are validated against the pinned issuer/client but the JWS signature
// is not checked (the token arrives over a direct TLS call to
// id.twitch.tv, which is the issuer-verification substitute RFC-wise;
// oauth4webapi.validateApplicationLevelSignature exists if that ever needs
// tightening).
//
// Nonce policy (tightened vs arctic-era code): when a nonce value is supplied,
// oauth4webapi asserts it matches the id_token nonce claim; when it is not,
// expectNoNonce asserts the token carries none. Callers must therefore always
// pass either the stored cookie nonce or expectNoNonce — a login whose
// oauth_nonce cookie vanished fails closed instead of skipping the check.
import {
  authorizationCodeGrantRequest,
  ClientSecretPost,
  expectNoNonce,
  generateRandomState,
  getValidatedIdTokenClaims,
  nopkce,
  OperationProcessingError,
  processAuthorizationCodeResponse,
  skipStateCheck,
  validateAuthResponse,
  type AuthorizationServer,
  type Client,
  type ClientAuth,
  type IDToken,
  ResponseBodyError
} from 'oauth4webapi';

export { ResponseBodyError, expectNoNonce };

// isOAuthProtocolError is what the auth callbacks should catch: BOTH failure
// families the exchange can produce. ResponseBodyError is the provider saying
// no (invalid grant, revoked code); OperationProcessingError is the provider
// answering something the strict parser refuses (the Twitch array-scope quirk
// was one). Neither is OUR server failing, so neither deserves the framework
// 500 — and a +server endpoint throw renders SvelteKit's bare fallback page,
// never the app's +error.svelte, which is exactly how the array-scope bug
// surfaced to users as an unstyled default 500.
export function isOAuthProtocolError(e: unknown): boolean {
  return e instanceof ResponseBodyError || e instanceof OperationProcessingError;
}
export const generateState = generateRandomState;

const TWITCH_AS: AuthorizationServer = {
  issuer: 'https://id.twitch.tv/oauth2',
  authorization_endpoint: 'https://id.twitch.tv/oauth2/authorize',
  token_endpoint: 'https://id.twitch.tv/oauth2/token'
};

export class OAuth2Tokens {
  constructor(private readonly result: Record<string, unknown>) {}

  accessToken(): string {
    if (typeof this.result.access_token === 'string') return this.result.access_token;
    throw new Error("Missing or invalid 'access_token' field");
  }

  refreshToken(): string {
    if (typeof this.result.refresh_token === 'string') return this.result.refresh_token;
    throw new Error("Missing or invalid 'refresh_token' field");
  }

  // For providers where a missing refresh token has a defined meaning (Google
  // re-consent) instead of being a protocol violation: absence is a value the
  // caller decides on, not an error.
  refreshTokenOptional(): string | undefined {
    return typeof this.result.refresh_token === 'string' ? this.result.refresh_token : undefined;
  }

  expiresIn(): number | undefined {
    return typeof this.result.expires_in === 'number' ? this.result.expires_in : undefined;
  }

  // Claims come back already validated (issuer, audience, expiry, nonce) by
  // processAuthorizationCodeResponse's ID Token processing.
  claims(): IDToken {
    const claims = getValidatedIdTokenClaims(this.result as never);
    if (!claims) throw new Error('Token response carried no ID Token claims');
    return claims;
  }
}

// normalizeTwitchScope rewrites the ONE way Twitch's token endpoint violates
// RFC 6749 before the strict library sees it: `scope` comes back as a JSON
// array (["openid", ...]) where §5.1 requires a space-delimited string.
// oauth4webapi rightly refuses it ('"response" body "scope" property must be
// a string'), which made EVERY successful login 500 — the exchange succeeds,
// then parsing throws OperationProcessingError, which is not the
// ResponseBodyError the callback maps to /login?e=oauth. Reproduced against a
// Twitch-shaped body and green with only this join applied.
//
// Only a 200 JSON body with an array scope is touched; error responses pass
// through byte-identical so ResponseBodyError classification stays the
// library's. This is vendor-quirk normalization at the boundary, not protocol
// logic — everything else stays inside oauth4webapi.
async function normalizeTwitchScope(response: Response): Promise<Response> {
  if (!response.ok) return response;
  const body: unknown = await response.clone().json().catch(() => null);
  if (typeof body !== 'object' || body === null) return response;
  const record = body as Record<string, unknown>;
  if (!Array.isArray(record.scope)) return response;
  record.scope = record.scope.join(' ');
  return new Response(JSON.stringify(record), {
    status: response.status,
    headers: response.headers
  });
}

// Exported for the regression test only; production code reaches it through
// Twitch.validateAuthorizationCode.
export const __normalizeTwitchScopeForTests = normalizeTwitchScope;

export class Twitch {
  private readonly client: Client;
  private readonly clientAuth: ClientAuth;

  constructor(
    private readonly clientId: string,
    clientSecret: string,
    private readonly redirectURI: string
  ) {
    this.client = { client_id: clientId };
    // client_secret_post matches what Twitch expects and what arctic sent:
    // credentials in the form body, not an Authorization header.
    this.clientAuth = ClientSecretPost(clientSecret);
  }

  createAuthorizationURL(state: string, scopes: string[]): URL {
    const url = new URL(TWITCH_AS.authorization_endpoint!);
    url.searchParams.set('response_type', 'code');
    url.searchParams.set('client_id', this.clientId);
    url.searchParams.set('state', state);
    if (scopes.length > 0) url.searchParams.set('scope', scopes.join(' '));
    url.searchParams.set('redirect_uri', this.redirectURI);
    return url;
  }

  async validateAuthorizationCode(
    code: string,
    nonce?: string | typeof expectNoNonce
  ): Promise<OAuth2Tokens> {
    // validateAuthResponse is the library's own callback sanity gate (rejects
    // provider error responses, missing code); state was already compared
    // against the HttpOnly cookie by the routes, so it's skipped here.
    const callbackParameters = validateAuthResponse(
      TWITCH_AS,
      this.client,
      new URLSearchParams({ code }),
      skipStateCheck
    );
    const response = await authorizationCodeGrantRequest(
      TWITCH_AS,
      this.client,
      this.clientAuth,
      callbackParameters,
      this.redirectURI,
      nopkce
    );
    const result = await processAuthorizationCodeResponse(
      TWITCH_AS,
      this.client,
      await normalizeTwitchScope(response),
      {
        requireIdToken: true,
        ...(nonce ? { expectedNonce: nonce } : {})
      }
    );
    return new OAuth2Tokens(result as unknown as Record<string, unknown>);
  }
}

// Same pinned-metadata trust model as Twitch above. Google is plain OAuth2 for
// this flow (no openid scope -> no id_token to validate), so the exchange runs
// the OAuth2-only response path and carries no nonce handling. CSRF stays with
// the routes' state cookie; PKCE stays off exactly like the Twitch flow.
const GOOGLE_AS: AuthorizationServer = {
  issuer: 'https://accounts.google.com',
  authorization_endpoint: 'https://accounts.google.com/o/oauth2/v2/auth',
  token_endpoint: 'https://oauth2.googleapis.com/token'
};

export class Google {
  private readonly client: Client;
  private readonly clientAuth: ClientAuth;

  constructor(
    private readonly clientId: string,
    clientSecret: string,
    private readonly redirectURI: string
  ) {
    this.client = { client_id: clientId };
    // Google accepts client_secret_post (credentials in the form body), which
    // keeps both providers on the same auth method.
    this.clientAuth = ClientSecretPost(clientSecret);
  }

  createAuthorizationURL(state: string, scopes: string[]): URL {
    const url = new URL(GOOGLE_AS.authorization_endpoint!);
    url.searchParams.set('response_type', 'code');
    url.searchParams.set('client_id', this.clientId);
    url.searchParams.set('state', state);
    if (scopes.length > 0) url.searchParams.set('scope', scopes.join(' '));
    url.searchParams.set('redirect_uri', this.redirectURI);
    // access_type=offline asks for a refresh token at all; without
    // prompt=consent Google issues one only on the very first consent for a
    // given scope set, so every connect forces the consent screen to keep
    // re-connects from silently returning access-token-only responses.
    url.searchParams.set('access_type', 'offline');
    url.searchParams.set('prompt', 'consent');
    return url;
  }

  async validateAuthorizationCode(code: string): Promise<OAuth2Tokens> {
    const callbackParameters = validateAuthResponse(
      GOOGLE_AS,
      this.client,
      new URLSearchParams({ code }),
      skipStateCheck
    );
    const response = await authorizationCodeGrantRequest(
      GOOGLE_AS,
      this.client,
      this.clientAuth,
      callbackParameters,
      this.redirectURI,
      nopkce
    );
    const result = await processAuthorizationCodeResponse(GOOGLE_AS, this.client, response);
    return new OAuth2Tokens(result as unknown as Record<string, unknown>);
  }
}
