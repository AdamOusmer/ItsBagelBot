// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  authorizationCodeGrantRequest,
  ClientSecretBasic,
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

  refreshTokenOptional(): string | undefined {
    return typeof this.result.refresh_token === 'string' ? this.result.refresh_token : undefined;
  }

  claims(): IDToken {
    const claims = getValidatedIdTokenClaims(this.result as never);
    if (!claims) throw new Error('Token response carried no ID Token claims');
    return claims;
  }
}

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

const SPOTIFY_AS: AuthorizationServer = {
  issuer: 'https://accounts.spotify.com',
  authorization_endpoint: 'https://accounts.spotify.com/authorize',
  token_endpoint: 'https://accounts.spotify.com/api/token'
};

export class Spotify {
  private readonly client: Client;
  private readonly clientAuth: ClientAuth;

  constructor(
    private readonly clientId: string,
    clientSecret: string,
    private readonly redirectURI: string
  ) {
    this.client = { client_id: clientId };
    this.clientAuth = ClientSecretBasic(clientSecret);
  }

  createAuthorizationURL(state: string, scopes: string[]): URL {
    const url = new URL(SPOTIFY_AS.authorization_endpoint!);
    url.searchParams.set('response_type', 'code');
    url.searchParams.set('client_id', this.clientId);
    url.searchParams.set('state', state);
    if (scopes.length > 0) url.searchParams.set('scope', scopes.join(' '));
    url.searchParams.set('redirect_uri', this.redirectURI);
    return url;
  }

  async validateAuthorizationCode(code: string): Promise<OAuth2Tokens> {
    const callbackParameters = validateAuthResponse(
      SPOTIFY_AS,
      this.client,
      new URLSearchParams({ code }),
      skipStateCheck
    );
    const response = await authorizationCodeGrantRequest(
      SPOTIFY_AS,
      this.client,
      this.clientAuth,
      callbackParameters,
      this.redirectURI,
      nopkce
    );
    const result = await processAuthorizationCodeResponse(SPOTIFY_AS, this.client, response);
    return new OAuth2Tokens(result as unknown as Record<string, unknown>);
  }
}
