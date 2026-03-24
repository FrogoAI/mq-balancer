package subscriber

import (
	"errors"
	"fmt"
	"testing"

	"github.com/FrogoAI/testutils"
)

var errTest = errors.New("test error")

func TestErrConnectionClosed(t *testing.T) {
	testutils.Equal(t, ErrConnectionClosed.Error(), "connection closed")
}

func TestErrConnectionClosed_Wrapping(t *testing.T) {
	wrapped := fmt.Errorf("%w: underlying", ErrConnectionClosed)
	testutils.Equal(t, errors.Is(wrapped, ErrConnectionClosed), true)
}
