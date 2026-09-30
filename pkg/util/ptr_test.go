package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPtr(t *testing.T) {
	v := "abc"
	i := 123

	assert.Equal(t, &v, Ptr("abc"))
	assert.Equal(t, &i, Ptr(123))
}

func TestVal(t *testing.T) {
	var tt time.Time
	var i int
	var s string

	assert.Equal(t, time.Time{}, Val(&tt))
	assert.Equal(t, time.Time{}, Val((*time.Time)(nil)))
	assert.Equal(t, 0, Val(&i))
	assert.Equal(t, 0, Val((*int)(nil)))
	assert.Equal(t, "", Val(&s))
	assert.Equal(t, "", Val((*string)(nil)))
}

func TestStrZeroPtr(t *testing.T) {
	v := "a"

	assert.Nil(t, StrZeroPtr(""))
	assert.Equal(t, &v, StrZeroPtr("a"))
}

func TestBoolValOrDefault_Default(t *testing.T) {
	assert.Equal(t, true, BoolValOrDefault(nil, true))
}

func TestBoolValOrDefault(t *testing.T) {
	v := true
	assert.Equal(t, v, BoolValOrDefault(&v, true))
}

func TestIntPtrOrDefault(t *testing.T) {
	a := assert.New(t)

	v := 1

	a.Nil(IntZeroPtr(0))
	a.Equal(&v, IntZeroPtr(v))
}

func TestPtrF(t *testing.T) {
	ptr := PtrF(func() int {
		return 42
	})
	assert.Equal(t, Ptr(42), ptr())
}
