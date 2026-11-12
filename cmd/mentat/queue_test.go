package main

import (
	"context"
	"testing"

	"github.com/hollis-labs/substrate/module"
)

func TestLazyQueueWithoutAppReturnsAnError(t *testing.T) {
	q := newLazyQueue(func() *module.App { return nil })
	if err := q.Push(context.Background(), "mentat.job", nil); err == nil {
		t.Fatal("expected an error for a nil app, got nil")
	}
	if _, err := q.Size(context.Background(), "default"); err == nil {
		t.Fatal("expected the error to be sticky")
	}
}
