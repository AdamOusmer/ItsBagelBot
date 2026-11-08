// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/internal/domain/validate"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/app/db/users/ent/deviceproof"
	"ItsBagelBot/app/db/users/tokensign"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/dpop"
)

const deviceTokenAAD = "bagel-device-access-v2"
const deviceNonceAAD = "bagel-device-nonce-v1"
const deviceRefreshAAD = "bagel-device-refresh-result-v1"
const deviceProofWindow = time.Minute
const deviceNonceLifetime = 2 * time.Minute

var ErrDeviceProof = errors.New("device authorization: invalid DPoP proof")
var ErrDeviceNonce = errors.New("device authorization: DPoP nonce required")

// DeviceAuthConfig contains trusted public request URLs. Forwarded Host headers
// never select a proof audience or move account credentials to another origin.
type DeviceAuthConfig struct {
	TokenURL     string
	ResourceURLs []string
}

func (r *Users) SetDeviceAuthConfig(config DeviceAuthConfig) {
	r.deviceAuth = DeviceAuthConfig{}
	if !validDeviceAuthURL(config.TokenURL) || len(config.ResourceURLs) > 8 {
		return
	}
	for _, url := range config.ResourceURLs {
		if !validDeviceAuthURL(url) {
			return
		}
	}
	r.deviceAuth = DeviceAuthConfig{TokenURL: config.TokenURL, ResourceURLs: append([]string(nil), config.ResourceURLs...)}
}
func validDeviceAuthURL(value string) bool {
	if len(value) > 180 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}

func (r *Users) sealDeviceValue(value []byte, aad string) (string, error) {
	if r.packer == nil {
		return "", ErrDeviceSigningKeyUnavailable
	}
	envelope, err := r.packer.Pack(value, []byte(aad))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(envelope.Ciphertext), nil
}
func (r *Users) openDeviceValue(value, aad string) ([]byte, error) {
	if r.packer == nil {
		return nil, ErrDeviceSigningKeyUnavailable
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, ErrDeviceInvalidGrant
	}
	return r.packer.Unpack(domaincrypto.SecureEnvelope{Ciphertext: raw, AttachedData: []byte(aad)})
}
func (r *Users) boundDeviceTokensFor(row *ent.DeviceGrant, refreshRaw []byte, now time.Time) (DeviceTokens, error) {
	payload := tokensign.Payload{Iss: tokensign.Issuer, Aud: tokensign.Audience, Sub: strconv.FormatUint(row.UserID, 10), DeviceID: row.DeviceID, GrantID: row.GrantID, Iat: now.Unix(), Exp: now.Add(deviceBoundAccessTokenTTL).Unix(), Jti: newDeviceUUID(), Confirmation: &tokensign.Confirmation{JKT: row.DpopJkt}}
	signed, err := r.deviceSigner.SignBound(payload)
	if err != nil {
		return DeviceTokens{}, err
	}
	sealed, err := r.sealDeviceValue([]byte(signed), deviceTokenAAD)
	if err != nil {
		return DeviceTokens{}, err
	}
	return DeviceTokens{AccessToken: "v2." + sealed, RefreshToken: deviceRefreshTokenPrefix + base64.RawURLEncoding.EncodeToString(refreshRaw), ExpiresIn: int64(deviceBoundAccessTokenTTL.Seconds()), GrantID: row.GrantID, DeviceID: row.DeviceID, TokenType: "DPoP"}, nil
}
func (r *Users) recoverBoundRefresh(row *ent.DeviceGrant) (DeviceTokens, error) {
	var result DeviceTokens
	raw, err := r.openDeviceValue(string(row.RefreshResult), deviceRefreshAAD)
	if err != nil {
		return result, err
	}
	if codec.Unmarshal(raw, &result) != nil || result.GrantID != row.GrantID || result.TokenType != "DPoP" {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	claims, err := r.openBoundAccess(result.AccessToken, time.Now().UTC())
	if err != nil {
		return DeviceTokens{}, err
	}
	result.ExpiresIn = claims.Exp - time.Now().UTC().Unix()
	return result, nil
}
func (r *Users) prepareBoundRotation(row *ent.DeviceGrant, raw []byte, now time.Time) (DeviceTokens, []byte, error) {
	tokens, err := r.boundDeviceTokensFor(row, raw, now)
	if err != nil {
		return tokens, nil, err
	}
	body, err := codec.Marshal(tokens)
	if err != nil {
		return DeviceTokens{}, nil, err
	}
	sealed, err := r.sealDeviceValue(body, deviceRefreshAAD)
	return tokens, []byte(sealed), err
}

type deviceNonce struct {
	Thumbprint string `json:"jkt"`
	URL        string `json:"url"`
	IssuedAt   int64  `json:"iat"`
	Random     string `json:"rnd"`
}

func (r *Users) proofNonce(jkt, url string, now time.Time) (string, error) {
	raw, err := codec.Marshal(deviceNonce{Thumbprint: jkt, URL: url, IssuedAt: now.Unix(), Random: newDeviceUUID()})
	if err != nil {
		return "", err
	}
	return r.sealDeviceValue(raw, deviceNonceAAD)
}
func (r *Users) validProofNonce(value, jkt, url string, now time.Time) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	raw, err := r.openDeviceValue(value, deviceNonceAAD)
	if err != nil {
		return false
	}
	var n deviceNonce
	if codec.Unmarshal(raw, &n) != nil {
		return false
	}
	return n.Thumbprint == jkt && n.URL == url && n.Random != "" && n.IssuedAt <= now.Unix()+30 && now.Unix()-n.IssuedAt <= int64(deviceNonceLifetime.Seconds())
}

// verifyDeviceProof validates first, then challenges with an encrypted,
// short-lived nonce. A SQL uniqueness constraint fences proof replay across pods.
func (r *Users) verifyDeviceProof(ctx context.Context, proof, method, url, token string) (string, string, error) {
	now := time.Now().UTC()
	claims, err := dpop.Verify(proof, dpop.Expected{Method: method, URL: url, Token: token, Now: now, Window: deviceProofWindow})
	if err != nil {
		return "", "", ErrDeviceProof
	}
	if !r.validProofNonce(claims.Nonce, claims.Thumbprint, url, now) {
		nonce, err := r.proofNonce(claims.Thumbprint, url, now)
		if err != nil {
			return "", "", err
		}
		return "", nonce, ErrDeviceNonce
	}
	hash := sha256.Sum256([]byte(claims.Thumbprint + "\x00" + claims.ID))
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.DeviceProof.Create().SetProofHash(hash[:]).SetExpiresAt(now.Add(5 * time.Minute)).Exec(ctx)
	})
	if ent.IsConstraintError(err) {
		return "", "", ErrDeviceProof
	}
	if err != nil {
		return "", "", err
	}
	return claims.Thumbprint, "", nil
}
func (r *Users) deleteExpiredDeviceProofs(ctx context.Context, now time.Time) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.DeviceProof.Delete().Where(deviceproof.ExpiresAtLT(now)).Exec(ctx)
	})
}

// AuthorizeDevice checks durable grant/owner state on every new binding and
// periodic HTTPS reauthorization; signature validity alone never revives a grant.
func (r *Users) AuthorizeDevice(ctx context.Context, token, proof, method, url string) (tokensign.Payload, string, error) {
	var empty tokensign.Payload
	if !r.allowedResourceURL(url) || r.deviceSigner == nil {
		return empty, "", ErrDeviceSigningKeyUnavailable
	}
	if method != "POST" && method != "GET" {
		return empty, "", ErrDeviceProof
	}
	jkt, nonce, err := r.verifyDeviceProof(ctx, proof, method, url, token)
	if err != nil {
		return empty, nonce, err
	}
	claims, err := r.openBoundAccess(token, time.Now().UTC())
	if err != nil {
		return empty, "", err
	}
	if claims.Confirmation.JKT != jkt {
		return empty, "", ErrDeviceProof
	}
	return r.authorizeBoundGrant(ctx, claims, jkt)
}
func (r *Users) authorizeBoundGrant(ctx context.Context, claims tokensign.Payload, jkt string) (tokensign.Payload, string, error) {
	var empty tokensign.Payload
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.DeviceGrant, error) {
		return r.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(claims.GrantID), devicegrant.DeviceIDEQ(claims.DeviceID), devicegrant.RevokedAtIsNil()).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return empty, "", ErrDeviceInvalidGrant
	}
	if err != nil {
		return empty, "", err
	}
	if strconv.FormatUint(row.UserID, 10) != claims.Sub || row.DpopJkt != jkt || deviceGrantExpired(row, time.Now().UTC()) {
		return empty, "", ErrDeviceInvalidGrant
	}
	if err = r.requireBoundDeviceOwner(ctx, row.UserID); err != nil {
		return empty, "", err
	}
	return claims, "", nil
}
func (r *Users) allowedResourceURL(url string) bool {
	for _, allowed := range r.deviceAuth.ResourceURLs {
		if allowed == url {
			return true
		}
	}
	return false
}
func (r *Users) openBoundAccess(token string, now time.Time) (tokensign.Payload, error) {
	var empty tokensign.Payload
	if !strings.HasPrefix(token, "v2.") || len(token) > 8192 {
		return empty, ErrDeviceInvalidGrant
	}
	raw, err := r.openDeviceValue(strings.TrimPrefix(token, "v2."), deviceTokenAAD)
	if err != nil {
		return empty, ErrDeviceInvalidGrant
	}
	claims, err := r.deviceSigner.VerifyBound(string(raw))
	if err != nil {
		return empty, ErrDeviceInvalidGrant
	}
	return validateBoundClaims(claims, now)
}
func validateBoundClaims(claims tokensign.Payload, now time.Time) (tokensign.Payload, error) {
	var empty tokensign.Payload
	if claims.Iss != tokensign.Issuer || claims.Aud != tokensign.Audience || claims.Confirmation == nil {
		return empty, ErrDeviceInvalidGrant
	}
	if claims.Exp <= now.Unix() || claims.Iat > now.Unix()+30 || claims.Exp-claims.Iat > int64(deviceBoundAccessTokenTTL.Seconds()) {
		return empty, ErrDeviceInvalidGrant
	}
	return validateBoundIdentity(claims)
}
func validateBoundIdentity(claims tokensign.Payload) (tokensign.Payload, error) {
	if claims.Iat <= 0 || claims.Exp <= claims.Iat || claims.Sub == "" || claims.DeviceID == "" || claims.GrantID == "" || claims.Jti == "" {
		return tokensign.Payload{}, ErrDeviceInvalidGrant
	}
	if validate.DeviceThumbprint(claims.Confirmation.JKT) != nil {
		return tokensign.Payload{}, ErrDeviceInvalidGrant
	}
	return claims, nil
}

func (r *Users) requireBoundDeviceOwner(ctx context.Context, id uint64) error {
	owner, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) { return r.client.User.Get(ctx, id) })
	if ent.IsNotFound(err) {
		return ErrDeviceInvalidGrant
	}
	if err != nil {
		return err
	}
	if owner.Banned {
		return ErrDeviceInvalidGrant
	}
	return nil
}

func (r *Users) saveBoundCodeResult(ctx context.Context, tx *ent.Tx, id string, tokens DeviceTokens) error {
	raw, err := codec.Marshal(tokens)
	if err != nil {
		return err
	}
	sealed, err := r.sealDeviceValue(raw, deviceRefreshAAD)
	if err != nil {
		return err
	}
	return tx.DeviceGrant.Update().Where(devicegrant.GrantIDEQ(id)).SetRefreshResult([]byte(sealed)).Exec(ctx)
}
