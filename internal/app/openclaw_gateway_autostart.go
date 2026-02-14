package app

import (
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ent0n29/samantha/internal/config"
)

func maybeAutoStartOpenClawGateway(cfg config.Config) (*exec.Cmd, string) {
	mode := strings.ToLower(strings.TrimSpace(cfg.OpenClawAdapterMode))
	if mode != "" && mode != "auto" && mode != "gateway" {
		return nil, ""
	}
	token := strings.TrimSpace(cfg.OpenClawGatewayToken)
	if token == "" {
		return nil, ""
	}

	rawURL := strings.TrimSpace(cfg.OpenClawGatewayURL)
	if rawURL == "" {
		rawURL = "ws://127.0.0.1:18789"
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, ""
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		host = "127.0.0.1"
	}
	// Only auto-start for loopback URLs; never spawn a gateway for remote hosts.
	if host != "127.0.0.1" && host != "localhost" {
		return nil, ""
	}

	port := strings.TrimSpace(u.Port())
	if port == "" {
		port = "18789"
	}
	addr := net.JoinHostPort(host, port)
	if isTCPListening(addr, 220*time.Millisecond) {
		return nil, ""
	}

	bin := strings.TrimSpace(cfg.OpenClawCLIPath)
	if bin == "" {
		bin = "openclaw"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, ""
	}

	cmd := exec.Command(bin, "gateway", "--allow-unconfigured", "--bind", "loopback", "--port", port)
	cmd.Env = append(os.Environ(), "OPENCLAW_GATEWAY_TOKEN="+token)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, ""
	}

	// Best-effort: wait briefly for the port to come up so the adapter can use it immediately.
	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if isTCPListening(addr, 160*time.Millisecond) {
			return cmd, addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cmd, addr
}

func isTCPListening(addr string, timeout time.Duration) bool {
	if strings.TrimSpace(addr) == "" {
		return false
	}
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func stopProcessBestEffort(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Try a graceful interrupt first.
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(700 * time.Millisecond):
		_ = cmd.Process.Kill()
		err := <-done
		if err == nil {
			return nil
		}
		// Ignore "already finished" style errors.
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}
