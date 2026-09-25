package main

import "testing"

// `runs list` and `runs cancel` must register --output like every other
// command in this package; before the fix, cobra rejected it at parse time
// with "unknown flag: --output" because init() never declared it here.
func TestRunsListAcceptsOutputFlag(t *testing.T) {
	if err := runsListCmd.Flags().Parse([]string{"--output", "json"}); err != nil {
		t.Fatalf("--output not registered on runs list: %v", err)
	}
	if v, _ := runsListCmd.Flags().GetString("output"); v != "json" {
		t.Fatalf("output = %q, want json", v)
	}
}

func TestRunsCancelAcceptsOutputFlag(t *testing.T) {
	if err := runsCancelCmd.Flags().Parse([]string{"--output", "json"}); err != nil {
		t.Fatalf("--output not registered on runs cancel: %v", err)
	}
	if v, _ := runsCancelCmd.Flags().GetString("output"); v != "json" {
		t.Fatalf("output = %q, want json", v)
	}
}
