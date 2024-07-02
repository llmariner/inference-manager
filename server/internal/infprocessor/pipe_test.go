package infprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipeReadWriteCloser(t *testing.T) {
	p := newPipeReadWriteCloser()
	go func() {
		_, err := p.Write([]byte("hello"))
		assert.NoError(t, err)
	}()

	buf := make([]byte, 5)
	n, err := p.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))

	err = p.Close()
	assert.NoError(t, err)
}
