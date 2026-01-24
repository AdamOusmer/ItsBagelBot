package crypto

type SecureEnvelope struct {
	Ciphertext   []byte `json:"ciphertext"`
	AttachedData []byte `json:"attached_data"`
}

type Packer interface {
	Pack(plaintext []byte, associatedData []byte) (SecureEnvelope, error)
	Unpack(envelope SecureEnvelope) ([]byte, error)
}
