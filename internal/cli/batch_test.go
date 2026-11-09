package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestBatchCommandValidation(t *testing.T) {
	root := NewRoot()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"batch"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when batch is called with no arguments")
	}
}

func TestBatchCommandUnknownSubcommand(t *testing.T) {
	root := NewRoot()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"batch", "nonexistent-action-xyz 123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got: %v", err)
	}
}

