package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectRuntime(t *testing.T) {
	tests := []struct {
		filename string
		runtime  string
	}{
		{"package.json", "node"},
		{"requirements.txt", "python"},
		{"pom.xml", "java"},
	}
	for _, test := range tests {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, test.filename), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
		if result := detectRuntime(directory); result != test.runtime {
			t.Fatalf("%s detected as %q", test.filename, result)
		}
	}
}

func TestParseBytes(t *testing.T) {
	tests := map[string]float64{
		"1MiB":  1024 * 1024,
		"2GiB":  2 * 1024 * 1024 * 1024,
		"500kB": 500000,
	}
	for input, expected := range tests {
		if result := parseBytes(input); result != expected {
			t.Fatalf("parseBytes(%q) = %f, expected %f", input, result, expected)
		}
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "../../outside"); err == nil {
		t.Fatal("expected path traversal to fail")
	}
}
