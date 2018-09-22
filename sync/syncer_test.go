package sync 

import (
	"testing"
)
func TestStartStop(t *testing.T) {
	s := New(nil, nil)

	if s == nil {
		t.Error("failed to create sync object")
	}
}