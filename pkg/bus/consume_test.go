package bus

import (
	"errors"
	"fmt"
	"testing"
)

type testExpectedNack struct{}

func (testExpectedNack) Error() string      { return "expected" }
func (testExpectedNack) ExpectedNack() bool { return true }

func TestIsExpectedNack(t *testing.T) {
	if !isExpectedNack(fmt.Errorf("wrapped: %w", testExpectedNack{})) {
		t.Fatal("wrapped expected nack was not recognized")
	}
	if isExpectedNack(errors.New("failure")) {
		t.Fatal("ordinary error was classified as expected")
	}
}
