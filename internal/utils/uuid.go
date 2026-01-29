package utils

import "github.com/google/uuid"

func NewID() (string, error) {
	id, err := uuid.NewV7()

	if err != nil {
		return "", err
	}
	return id.String(), nil
}
