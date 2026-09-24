// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"testing"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// aclKeyring holds the private nkeys behind a natsacl.Keys, so a test can
// both compile claims and sign them (accounts by the operator, roles by
// their own scoped signing key).
type aclKeyring struct {
	operator nkeys.KeyPair
	keys     *natsacl.Keys
	accounts map[string]nkeys.KeyPair
	roles    map[string]map[string]nkeys.KeyPair
}

func newACLKeyring(t *testing.T, acl *natsacl.ACL) *aclKeyring {
	t.Helper()
	operator := createKeyPair(t, nkeys.CreateOperator)
	operatorPub := publicKey(t, operator)

	ring := &aclKeyring{
		operator: operator,
		keys: &natsacl.Keys{
			Operator: operatorPub, Accounts: map[string]string{}, Roles: map[string]map[string]string{},
			Activations: map[string][]natsacl.Activation{},
		},
		accounts: map[string]nkeys.KeyPair{},
		roles:    map[string]map[string]nkeys.KeyPair{},
	}
	for name, spec := range acl.Accounts {
		ring.addAccount(t, name, spec)
	}
	return ring
}

func (r *aclKeyring) addAccount(t *testing.T, name string, spec natsacl.AccountSpec) {
	t.Helper()
	account := createKeyPair(t, nkeys.CreateAccount)
	r.accounts[name] = account
	r.keys.Accounts[name] = publicKey(t, account)
	if len(spec.Roles) == 0 {
		return
	}
	r.roles[name] = map[string]nkeys.KeyPair{}
	r.keys.Roles[name] = map[string]string{}
	for role := range spec.Roles {
		signer := createKeyPair(t, nkeys.CreateAccount)
		r.roles[name][role] = signer
		r.keys.Roles[name][role] = publicKey(t, signer)
	}
}

func createKeyPair(t *testing.T, create func() (nkeys.KeyPair, error)) nkeys.KeyPair {
	t.Helper()
	kp, err := create()
	if err != nil {
		t.Fatalf("create nkey: %v", err)
	}
	return kp
}

func publicKey(t *testing.T, kp nkeys.KeyPair) string {
	t.Helper()
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	return pub
}

// signedAccountJWTs mints real activations for acl's restricted exports,
// compiles acl with the keyring's public keys (Compile attaches each
// activation to its matching import), and signs every resulting account
// claim with the operator key.
func signedAccountJWTs(t *testing.T, acl *natsacl.ACL, ring *aclKeyring) map[string]string {
	t.Helper()
	populateActivations(t, acl, ring)
	claims, err := natsacl.Compile(acl, ring.keys)
	if err != nil {
		t.Fatalf("compile acl: %v", err)
	}

	tokens := make(map[string]string, len(claims))
	for _, c := range claims {
		token, err := c.Encode(ring.operator)
		if err != nil {
			t.Fatalf("encode account %s: %v", c.Name, err)
		}
		tokens[c.Name] = token
	}
	return tokens
}

// newRoleUser mints a JWT+seed for a fresh user under account's role, signed
// by that role's scoped signing key exactly as a real deployment would.
func newRoleUser(t *testing.T, ring *aclKeyring, account, role string) (userJWT, userSeed string) {
	t.Helper()
	user := createKeyPair(t, nkeys.CreateUser)
	seed, err := user.Seed()
	if err != nil {
		t.Fatalf("read user seed: %v", err)
	}

	claims := jwt.NewUserClaims(publicKey(t, user))
	claims.IssuerAccount = ring.keys.Accounts[account]
	claims.SetScoped(true)

	signer, ok := ring.roles[account][role]
	if !ok {
		t.Fatalf("no signing key for %s role %s", account, role)
	}
	token, err := claims.Encode(signer)
	if err != nil {
		t.Fatalf("encode user for %s/%s: %v", account, role, err)
	}
	return token, string(seed)
}
