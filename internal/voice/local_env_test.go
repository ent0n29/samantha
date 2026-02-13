package voice

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrependPathEnv(t *testing.T) {
	got := prependPathEnv([]string{"A=1"}, "DYLD_FALLBACK_LIBRARY_PATH", "/tmp/lib")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "DYLD_FALLBACK_LIBRARY_PATH=/tmp/lib") {
		t.Fatalf("missing prepended env, got: %v", got)
	}

	got = prependPathEnv([]string{"DYLD_FALLBACK_LIBRARY_PATH=/opt/lib"}, "DYLD_FALLBACK_LIBRARY_PATH", "/tmp/lib")
	joined = strings.Join(got, "\n")
	if !strings.Contains(joined, "DYLD_FALLBACK_LIBRARY_PATH=/tmp/lib:/opt/lib") {
		t.Fatalf("missing prefixed path, got: %v", got)
	}

	got = prependPathEnv([]string{"DYLD_FALLBACK_LIBRARY_PATH=/tmp/lib:/opt/lib"}, "DYLD_FALLBACK_LIBRARY_PATH", "/tmp/lib")
	joined = strings.Join(got, "\n")
	if strings.Count(joined, "/tmp/lib") != 1 {
		t.Fatalf("duplicate path added, got: %v", got)
	}
}

func TestInjectWhisperLibraryEnv(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	libDir := filepath.Join(root, "lib")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib: %v", err)
	}

	toolPath := filepath.Join(binDir, "whisper-cli")
	if err := os.WriteFile(toolPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	cmd := exec.Command("echo", "ok")
	cmd.Env = []string{"DYLD_FALLBACK_LIBRARY_PATH=/opt/lib"}
	injectWhisperLibraryEnv(cmd, toolPath)

	env := strings.Join(cmd.Env, "\n")
	wantPrefix := "DYLD_FALLBACK_LIBRARY_PATH=" + libDir + ":/opt/lib"
	if !strings.Contains(env, wantPrefix) {
		t.Fatalf("fallback path not injected, want prefix %q got %v", wantPrefix, cmd.Env)
	}
	if !strings.Contains(env, "DYLD_LIBRARY_PATH="+libDir) {
		t.Fatalf("dyld library path not injected, got %v", cmd.Env)
	}
}
