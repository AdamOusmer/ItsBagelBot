// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nkeys"
	"gopkg.in/yaml.v3"
)

var keysYAMLHeader = []byte("# Copyright (c) 2026 Adam Ousmer. All rights reserved.\n# Proprietary. No license granted. See LICENSE.md.\n\n")

func buildKeys(mat *material, activations map[string][]natsacl.Activation) (*natsacl.Keys, error) {
	operatorPub, err := mat.operator.PublicKey()
	if err != nil {
		return nil, err
	}
	accounts, err := publicKeys(mat.accounts)
	if err != nil {
		return nil, err
	}
	roles := make(map[string]map[string]string, len(mat.roles))
	for account, keys := range mat.roles {
		rolePubs, err := publicKeys(keys)
		if err != nil {
			return nil, err
		}
		roles[account] = rolePubs
	}
	return &natsacl.Keys{
		Operator:    operatorPub,
		Accounts:    accounts,
		Roles:       roles,
		Activations: activations,
	}, nil
}

func publicKeys(kps map[string]nkeys.KeyPair) (map[string]string, error) {
	out := make(map[string]string, len(kps))
	for name, kp := range kps {
		pub, err := kp.PublicKey()
		if err != nil {
			return nil, fmt.Errorf("natscreds: public key for %s: %w", name, err)
		}
		out[name] = pub
	}
	return out, nil
}

func writeKeysYAML(path string, keys *natsacl.Keys) error {
	body, err := yaml.Marshal(keys)
	if err != nil {
		return fmt.Errorf("natscreds: marshal %s: %w", path, err)
	}
	return os.WriteFile(path, append(append([]byte{}, keysYAMLHeader...), body...), 0o644)
}

func writeOperatorJWT(path, token string) error {
	return os.WriteFile(path, []byte(token+"\n"), 0o644)
}
