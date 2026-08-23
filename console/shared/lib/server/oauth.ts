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

  // Claims come back already validated (issuer, audience, expiry, nonce) by
  // processAuthorizationCodeResponse's ID Token processing.
  claims(): IDToken {
    const claims = getValidatedIdTokenClaims(this.result as never);
    if (!claims) throw new Error('Token response carried no ID Token claims');
    return claims;
  }
}

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
    const result = await processAuthorizationCodeResponse(TWITCH_AS, this.client, response, {
      requireIdToken: true,
      ...(nonce ? { expectedNonce: nonce } : {})
    });
    return new OAuth2Tokens(result as unknown as Record<string, unknown>);
  }
}
