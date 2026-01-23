package crypto

type SecureEnvelope struct {
	Ciphertext   []byte `json:"ciphertext"`
	AttachedData []byte `json:"attached_data"`
	ID           string `json:"id"`
}

type Packer interface {
	Pack(plaintext []byte, associatedData []byte) (SecureEnvelope, error)
}
