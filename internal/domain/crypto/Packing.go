// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package crypto

type SecureEnvelope struct {
	Ciphertext   []byte `json:"ciphertext"`
	AttachedData []byte `json:"attached_data"`
}

type Packer interface {
	Pack(plaintext []byte, associatedData []byte) (SecureEnvelope, error)
	Unpack(envelope SecureEnvelope) ([]byte, error)
}
