package util

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
)

func TestForEachGo(t *testing.T) {
	lock := sync.RWMutex{}
	codes := []string{"A", "B"}
	out := []string{}
	ForEachGo(codes, func(code string) {
		lock.Lock()
		out = append(out, code)
		lock.Unlock()
	})
	assert.Equal(t, 2, len(out))
	assert.Contains(t, out, "A")
	assert.Contains(t, out, "B")
}

func TestForEachGoErr(t *testing.T) {
	lock := sync.RWMutex{}
	codes := []string{"A", "B", "C", "D"}

	t.Run("happy flow", func(t *testing.T) {
		var out []string
		err := ForEachGoErr(codes, func(code string) error {
			lock.Lock()
			out = append(out, code)
			lock.Unlock()
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 4, len(out))
		assert.Contains(t, out, "A")
		assert.Contains(t, out, "B")
		assert.Contains(t, out, "C")
		assert.Contains(t, out, "D")
	})
	t.Run("one flow fails", func(t *testing.T) {
		var out []string
		err := ForEachGoErr(codes, func(code string) error {
			switch code {
			case "C":
				return errors.NewInternalError("C not supported")
			default:
				lock.Lock()
				out = append(out, code)
				lock.Unlock()
			}
			return nil
		})
		assert.Error(t, err)
		assert.ErrorContains(t, err, "C not supported")
		assert.Equal(t, 3, len(out))
		assert.Contains(t, out, "A")
		assert.Contains(t, out, "B")
		assert.Contains(t, out, "D")
	})
}

func TestAny(t *testing.T) {
	assert.True(t, Any([]string{"1", "2", "3"}, func(i string) bool { return i == "2" }))
	assert.False(t, Any([]string{"1", "2", "3"}, func(i string) bool { return i == "4" }))
}

func TestFilter(t *testing.T) {
	assert.Equal(t, []string{"1", "11"}, Filter([]string{"1", "2", "3", "11"}, func(i string) bool { return strings.HasPrefix(i, "1") }))
	assert.Equal(t, []string{}, Filter([]string{"1", "2", "3", "11"}, func(i string) bool { return strings.HasPrefix(i, "6") }))
}

func TestFind(t *testing.T) {
	assert.Equal(t, "2", Find([]string{"1", "2", "3"}, func(i string) bool { return i == "2" }))
	assert.Equal(t, "", Find([]string{"1", "2", "3"}, func(i string) bool { return i == "4" }))
}

func TestFindOk(t *testing.T) {
	t.Run("simple type", func(t *testing.T) {
		res, ok := FindOk([]string{"1", "2", "3"}, func(i string) bool { return i == "2" })
		assert.True(t, ok)
		assert.Equal(t, "2", res)

		res, ok = FindOk([]string{"1", "2", "3"}, func(i string) bool { return i == "4" })
		assert.False(t, ok)
		assert.Equal(t, "", res)
	})
	t.Run("complex type", func(t *testing.T) {
		type data struct {
			code string
		}
		res, ok := FindOk([]*data{{code: "A"}, {code: "B"}}, func(d *data) bool { return d.code == "B" })
		assert.True(t, ok)
		assert.Equal(t, &data{code: "B"}, res)

		res, ok = FindOk([]*data{{code: "A"}, {code: "B"}}, func(d *data) bool { return d.code == "C" })
		assert.False(t, ok)
		assert.Equal(t, (*data)(nil), res)
	})
}

func TestMap(t *testing.T) {
	assert.Equal(t, []int{1, 2, 3}, Map([]string{"1", "2", "3"}, func(i string) int {
		out, _ := strconv.Atoi(i)
		return out
	}))
}

func TestMapErr(t *testing.T) {
	is, err := MapErr([]string{"1", "2", "3"}, func(i string) (int, error) {
		return strconv.Atoi(i)
	})

	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, is)
}

func TestMapErr_Fail(t *testing.T) {
	is, err := MapErr([]string{"not", "an", "int"}, func(i string) (int, error) {
		return strconv.Atoi(i)
	})

	assert.Error(t, err)
	assert.Equal(t, []int(nil), is)
}

func TestMapGoErr(t *testing.T) {
	t.Run("happy flow", func(t *testing.T) {
		out, err := MapGoErr([]string{"1", "2", "3", "4"}, func(i string) (int, error) {
			return strconv.Atoi(i)
		})

		assert.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3, 4}, out)
	})

	t.Run("empty input", func(t *testing.T) {
		out, err := MapGoErr([]string{}, func(i string) (int, error) {
			return strconv.Atoi(i)
		})

		assert.NoError(t, err)
		assert.Equal(t, []int{}, out)
	})

	t.Run("one flow fails", func(t *testing.T) {
		out, err := MapGoErr([]string{"1", "2", "C", "4"}, func(i string) (int, error) {
			if i == "C" {
				return 0, errors.NewInternalError("C not supported")
			}
			return strconv.Atoi(i)
		})

		assert.Error(t, err)
		assert.ErrorContains(t, err, "C not supported")
		assert.Equal(t, []int(nil), out)
	})
}

func TestCopySlice(t *testing.T) {
	s := []string{"1", "2", "3"}

	res := CopySlice(s)

	assert.Equal(t, s, res)
}

func TestChunk(t *testing.T) {
	assert.Equal(t, [][]string{{"1", "2"}, {"3", "4"}, {"5"}}, Chunk([]string{"1", "2", "3", "4", "5"}, 2))
	assert.Equal(t, [][]string{{"1"}}, Chunk([]string{"1"}, 2))
}

func TestFlatMap(t *testing.T) {
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, lo.FlatMap([][]string{{"1", "2"}, {"3", "4"}, {"5"}}, func(ss []string, _ int) []string {
		return ss
	}))
}

func TestOrDefault(t *testing.T) {
	t.Run("zero int value", func(t *testing.T) {
		assert.Equal(t, 1, OrDefault(0, 1))
	})
	t.Run("non zero int value", func(t *testing.T) {
		assert.Equal(t, 25, OrDefault(25, 1))
	})
	t.Run("zero string value", func(t *testing.T) {
		assert.Equal(t, "something", OrDefault("", "something"))
	})
	t.Run("non zero string value", func(t *testing.T) {
		assert.Equal(t, "set", OrDefault("set", "something"))
	})
}
