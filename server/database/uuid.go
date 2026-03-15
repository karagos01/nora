package database

import "github.com/google/uuid"

func newUUID() (string, error) {
	id, err := uuid.NewV7()
	return id.String(), err
}
