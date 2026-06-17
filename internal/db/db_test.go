package db

import (
	"context"
	"errors"
	"testing"
)

func TestOpenRejectsEmptyDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), "")
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Fatalf("expected ErrMissingDatabaseURL, got %v", err)
	}
}
