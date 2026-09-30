package util

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

var (
	// Human readable UUIDs, which are still valid UUID v8 (custom)
	MockUUID1 = uuid.MustParse("00000000-0000-8000-8000-000000000001")
	MockUUID2 = uuid.MustParse("00000000-0000-8000-8000-000000000002")
	MockUUID3 = uuid.MustParse("00000000-0000-8000-8000-000000000003")
)

type MockUUIDGenerator struct {
	mock.Mock
}

func (m *MockUUIDGenerator) Generate() string {
	a := m.Called()
	return a.Get(0).(string)
}

func (m *MockUUIDGenerator) GenerateUUID() uuid.UUID {
	a := m.Called()
	return a.Get(0).(uuid.UUID)
}

type StaticUUIDGenerator struct{}

func (m *StaticUUIDGenerator) Generate() string {
	return "static-uuid"
}
