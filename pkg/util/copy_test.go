package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	ptr "gitlab.com/sqills/development/vectron/golang-packages/pkg/util"
)

type testStruct struct {
	a, b string
	p    *int
}

func TestCopy(t *testing.T) {
	a := assert.New(t)

	init := &testStruct{"a", "b", ptr.Ptr(123)}
	copyRes := Copy(init, func(item *testStruct) {
		item.a = "c"
		item.b = "d"
	})

	a.NotSame(init, copyRes)
	a.Same(init.p, copyRes.p) // shallow copy
	a.NotEqual(init, copyRes)
	a.NotEqual(init.a, copyRes.a)
	a.NotEqual(init.b, copyRes.b)
}
