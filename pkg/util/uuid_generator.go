package util

import (
	"github.com/google/uuid"
)

type IDGenerator interface {
	Generate() string
}

type UUIDGenerator interface {
	IDGenerator
	GenerateUUID() uuid.UUID
}

type uuidGenerator struct{}

func NewUUIDGenerator() UUIDGenerator {
	return &uuidGenerator{}
}

func (c *uuidGenerator) Generate() string {
	return uuid.NewString()
}

func (c *uuidGenerator) GenerateUUID() uuid.UUID {
	return uuid.New()
}

func UUIDv5(input string) string {
	return uuid.NewSHA1(uuid.Nil, []byte(input)).String()
}
