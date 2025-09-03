// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package ringbuffer_test

import (
	"testing"

	"github.com/antimetal/agent/pkg/ringbuffer"
	"github.com/stretchr/testify/assert"
)

func TestRingBuffer(t *testing.T) {
	// Test basic functionality
	t.Run("basic push and getAll", func(t *testing.T) {
		rb, err := ringbuffer.New[int](3)
		assert.NoError(t, err)

		// Empty buffer
		assert.Equal(t, []int{}, rb.GetAll())
		assert.Equal(t, 0, rb.Len())
		assert.Equal(t, 3, rb.Cap())

		// Add one element
		rb.Push(1)
		assert.Equal(t, []int{1}, rb.GetAll())
		assert.Equal(t, 1, rb.Len())

		// Add more elements
		rb.Push(2)
		rb.Push(3)
		assert.Equal(t, []int{1, 2, 3}, rb.GetAll())
		assert.Equal(t, 3, rb.Len())
	})

	// Test overflow behavior
	t.Run("overflow wraps around", func(t *testing.T) {
		rb, err := ringbuffer.New[string](3)
		assert.NoError(t, err)

		// Fill buffer
		rb.Push("a")
		rb.Push("b")
		rb.Push("c")
		assert.Equal(t, []string{"a", "b", "c"}, rb.GetAll())
		assert.Equal(t, 3, rb.Len())

		// Overflow - should drop oldest
		rb.Push("d")
		assert.Equal(t, []string{"b", "c", "d"}, rb.GetAll())
		assert.Equal(t, 3, rb.Len())

		rb.Push("e")
		rb.Push("f")
		assert.Equal(t, []string{"d", "e", "f"}, rb.GetAll())
	})

	// Test large buffer
	t.Run("large buffer", func(t *testing.T) {
		rb, err := ringbuffer.New[int](1000)
		assert.NoError(t, err)

		// Add 500 elements
		for i := 0; i < 500; i++ {
			rb.Push(i)
		}

		result := rb.GetAll()
		assert.Len(t, result, 500)
		assert.Equal(t, 0, result[0])
		assert.Equal(t, 499, result[499])

		// Add 600 more (total 1100, should keep last 1000)
		for i := 500; i < 1100; i++ {
			rb.Push(i)
		}

		result = rb.GetAll()
		assert.Len(t, result, 1000)
		assert.Equal(t, 100, result[0]) // First 100 dropped
		assert.Equal(t, 1099, result[999])
		assert.Equal(t, 1000, rb.Len())
	})

	// Test clear functionality
	t.Run("clear buffer", func(t *testing.T) {
		rb, err := ringbuffer.New[int](5)
		assert.NoError(t, err)

		// Add elements
		for i := 0; i < 10; i++ {
			rb.Push(i)
		}

		assert.Equal(t, 5, rb.Len())
		assert.Equal(t, []int{5, 6, 7, 8, 9}, rb.GetAll())

		// Clear buffer
		rb.Clear()
		assert.Equal(t, 0, rb.Len())
		assert.Equal(t, []int{}, rb.GetAll())

		// Can add elements again after clear
		rb.Push(100)
		rb.Push(200)
		assert.Equal(t, 2, rb.Len())
		assert.Equal(t, []int{100, 200}, rb.GetAll())
	})

	// Test with different types
	t.Run("struct type", func(t *testing.T) {
		type testStruct struct {
			id   int
			name string
		}

		rb, err := ringbuffer.New[testStruct](2)
		assert.NoError(t, err)

		rb.Push(testStruct{1, "first"})
		rb.Push(testStruct{2, "second"})
		rb.Push(testStruct{3, "third"})

		result := rb.GetAll()
		assert.Len(t, result, 2)
		assert.Equal(t, testStruct{2, "second"}, result[0])
		assert.Equal(t, testStruct{3, "third"}, result[1])
	})

	// Test edge cases
	t.Run("single element buffer", func(t *testing.T) {
		rb, err := ringbuffer.New[int](1)
		assert.NoError(t, err)

		rb.Push(1)
		assert.Equal(t, []int{1}, rb.GetAll())

		rb.Push(2)
		assert.Equal(t, []int{2}, rb.GetAll())

		rb.Push(3)
		assert.Equal(t, []int{3}, rb.GetAll())
	})

	// Test error cases
	t.Run("invalid capacity", func(t *testing.T) {
		// Zero capacity
		rb, err := ringbuffer.New[int](0)
		assert.Error(t, err)
		assert.Nil(t, rb)
		assert.Contains(t, err.Error(), "capacity must be greater than 0, got 0")

		// Negative capacity
		rb, err = ringbuffer.New[int](-5)
		assert.Error(t, err)
		assert.Nil(t, rb)
		assert.Contains(t, err.Error(), "capacity must be greater than 0, got -5")
	})
}
