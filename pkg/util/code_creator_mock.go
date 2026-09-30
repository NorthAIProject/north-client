package util

import (
	"github.com/stretchr/testify/mock"
)

type MockCodeCreator struct {
	mock.Mock
}

func (m *MockCodeCreator) Create(pattern string) (string, error) {
	a := m.Called(pattern)
	return a.Get(0).(string), a.Error(1)
}

func (m *MockCodeCreator) Validate(pattern, code string) error {
	a := m.Called(pattern, code)
	return a.Error(0)
}
