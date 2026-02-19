package openclaw

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	gatewayConnectWriteTimeout = 3 * time.Second
	gatewayAgentWriteTimeout   = 2 * time.Second
)

// GatewayAdapter streams responses from the OpenClaw Gateway WebSocket protocol.
//
// This is the only OpenClaw path that reliably yields early assistant deltas
// (vs waiting for the full CLI turn to complete).
type GatewayAdapter struct {
	wsURL          string
	token          string
	thinking       string
	streamMinChars int

	deviceID     string
	publicKeyB64 string
	privateKey   ed25519.PrivateKey

	clientID      string
	clientMode    string
	clientVersion string
	platform      string
	locale        string
	userAgent     string

	dialer websocket.Dialer

	poolMu sync.Mutex
	pool   map[string]*pooledGatewayConn
}

type gatewayFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Event   string          `json:"event,omitempty"`
	OK      bool            `json:"ok,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *gatewayError   `json:"error,omitempty"`
}

type gatewayError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type connectChallengePayload struct {
	Nonce string `json:"nonce"`
	TS    int64  `json:"ts"`
}

type connectClient struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Mode     string `json:"mode"`
}

type connectAuth struct {
	Token string `json:"token,omitempty"`
}

type connectDevice struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce,omitempty"`
}

type connectParams struct {
	MinProtocol int            `json:"minProtocol"`
	MaxProtocol int            `json:"maxProtocol"`
	Client      connectClient  `json:"client"`
	Role        string         `json:"role"`
	Scopes      []string       `json:"scopes"`
	Caps        []string       `json:"caps"`
	Commands    []string       `json:"commands"`
	Permissions map[string]any `json:"permissions"`
	Auth        *connectAuth   `json:"auth,omitempty"`
	Locale      string         `json:"locale,omitempty"`
	UserAgent   string         `json:"userAgent,omitempty"`
	Device      connectDevice  `json:"device"`
}

type gatewayRequest struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type agentParams struct {
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotencyKey"`
	AgentID        string `json:"agentId,omitempty"`
	SessionKey     string `json:"sessionKey,omitempty"`
	Thinking       string `json:"thinking,omitempty"`
}

type agentEventPayload struct {
	RunID      string          `json:"runId"`
	Stream     string          `json:"stream"`
	Data       json.RawMessage `json:"data"`
	SessionKey string          `json:"sessionKey"`
}

type agentAssistantData struct {
	Text  string `json:"text"`
	Delta string `json:"delta"`
}

type agentLifecycleData struct {
	Phase     string `json:"phase"`
	StartedAt int64  `json:"startedAt,omitempty"`
	EndedAt   int64  `json:"endedAt,omitempty"`
	Message   string `json:"message,omitempty"`
}

type openclawDeviceIdentity struct {
	Version       int    `json:"version"`
	DeviceID      string `json:"deviceId"`
	PublicKeyPem  string `json:"publicKeyPem"`
	PrivateKeyPem string `json:"privateKeyPem"`
	CreatedAtMs   int64  `json:"createdAtMs"`
}

func NewGatewayAdapter(wsURL, token, thinking string, streamMinChars int) (*GatewayAdapter, error) {
	wsURL, err := normalizeGatewayURL(wsURL)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("openclaw gateway token is required")
	}

	ident, err := loadOpenClawDeviceIdentity()
	if err != nil {
		return nil, err
	}
	priv, pubB64, err := parseOpenClawEd25519Keys(ident)
	if err != nil {
		return nil, err
	}

	platform := strings.ToLower(strings.TrimSpace(runtime.GOOS))
	// OpenClaw expects "darwin" (not "macos") for macOS clients.
	if platform == "" {
		platform = "unknown"
	}

	return &GatewayAdapter{
		wsURL:          wsURL,
		token:          token,
		thinking:       normalizeThinkingLevel(thinking),
		streamMinChars: normalizeStreamMinChars(streamMinChars),
		deviceID:       ident.DeviceID,
		publicKeyB64:   pubB64,
		privateKey:     priv,
		clientID:       "cli",
		clientMode:     "cli",
		clientVersion:  "dev",
		platform:       platform,
		locale:         "en-US",
		userAgent:      "samantha",
		dialer: websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 4 * time.Second,
		},
		pool: make(map[string]*pooledGatewayConn),
	}, nil
}

func normalizeGatewayURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "ws://127.0.0.1:18789"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse OPENCLAW_GATEWAY_URL: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported gateway url scheme %q", u.Scheme)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func openclawStateDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR")); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("cannot resolve home directory for openclaw state")
	}
	return filepath.Join(home, ".openclaw"), nil
}

func loadOpenClawDeviceIdentity() (openclawDeviceIdentity, error) {
	stateDir, err := openclawStateDir()
	if err != nil {
		return openclawDeviceIdentity{}, err
	}
	path := filepath.Join(stateDir, "identity", "device.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return openclawDeviceIdentity{}, fmt.Errorf("read openclaw device identity %s: %w", path, err)
	}
	var ident openclawDeviceIdentity
	if err := json.Unmarshal(raw, &ident); err != nil {
		return openclawDeviceIdentity{}, fmt.Errorf("parse openclaw device identity %s: %w", path, err)
	}
	ident.DeviceID = strings.TrimSpace(ident.DeviceID)
	if ident.DeviceID == "" {
		return openclawDeviceIdentity{}, fmt.Errorf("openclaw device identity missing deviceId")
	}
	if strings.TrimSpace(ident.PublicKeyPem) == "" || strings.TrimSpace(ident.PrivateKeyPem) == "" {
		return openclawDeviceIdentity{}, fmt.Errorf("openclaw device identity missing keys")
	}
	return ident, nil
}

func parseOpenClawEd25519Keys(ident openclawDeviceIdentity) (ed25519.PrivateKey, string, error) {
	priv, err := parseEd25519PrivateKeyFromPEM(ident.PrivateKeyPem)
	if err != nil {
		return nil, "", err
	}
	pub, err := parseEd25519PublicKeyFromPEM(ident.PublicKeyPem)
	if err != nil {
		return nil, "", err
	}
	return priv, base64.StdEncoding.EncodeToString(pub), nil
}

func parseEd25519PrivateKeyFromPEM(pemStr string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("openclaw private key PEM decode failed")
	}
	pk, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("openclaw private key parse: %w", err)
	}
	ed, ok := pk.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("openclaw private key is %T, want ed25519", pk)
	}
	return ed, nil
}

func parseEd25519PublicKeyFromPEM(pemStr string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("openclaw public key PEM decode failed")
	}
	pk, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("openclaw public key parse: %w", err)
	}
	ed, ok := pk.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("openclaw public key is %T, want ed25519", pk)
	}
	return ed, nil
}

// connectSignatureString matches OpenClaw Gateway's "device identity" signing input.
// See dist client code (control-ui) for the reference implementation.
func connectSignatureString(deviceID, clientID, clientMode, role string, scopes []string, signedAtMs int64, token, nonce string) string {
	version := "v1"
	if strings.TrimSpace(nonce) != "" {
		version = "v2"
	}
	scopeCSV := strings.Join(scopes, ",")
	parts := []string{
		version,
		deviceID,
		clientID,
		clientMode,
		role,
		scopeCSV,
		fmt.Sprintf("%d", signedAtMs),
		token,
	}
	if version == "v2" {
		parts = append(parts, nonce)
	}
	return strings.Join(parts, "|")
}

func (a *GatewayAdapter) StreamResponse(ctx context.Context, req MessageRequest, onDelta DeltaHandler) (MessageResponse, error) {
	prompt := buildPrompt(req)
	if strings.TrimSpace(prompt) == "" {
		return MessageResponse{}, nil
	}

	agentID := resolvedGatewayAgentID()

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "main"
	}
	sessionKey := gatewaySessionKey(agentID, sessionID)

	runID := strings.TrimSpace(req.TurnID)
	if runID == "" {
		runID = uuid.NewString()
	}

	if strings.HasPrefix(runID, "spec-") {
		conn, ws, err := a.dialAndConnect(ctx)
		if err != nil {
			return MessageResponse{}, err
		}
		defer conn.Close()

		resp, _, err := a.streamAgent(ctx, conn, ws, prompt, agentID, sessionKey, runID, onDelta)
		return resp, err
	}

	pc := a.getPooledConn(sessionKey)
	resp, keep, err := pc.stream(ctx, a, prompt, agentID, sessionKey, runID, onDelta)
	if !keep {
		a.dropPooledConn(sessionKey, pc)
	}
	return resp, err
}

// PrewarmSession establishes and caches a connected gateway websocket for the
// given session key so the first real turn can skip handshake latency.
func (a *GatewayAdapter) PrewarmSession(ctx context.Context, sessionID string) error {
	if a == nil {
		return errors.New("gateway adapter is nil")
	}
	agentID := resolvedGatewayAgentID()
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "main"
	}
	sessionKey := gatewaySessionKey(agentID, sessionID)
	pc := a.getPooledConn(sessionKey)
	if err := pc.ensureConnected(ctx, a); err != nil {
		a.dropPooledConn(sessionKey, pc)
		return err
	}
	return nil
}

func resolvedGatewayAgentID() string {
	agentID := strings.TrimSpace(os.Getenv("OPENCLAW_AGENT_ID"))
	if agentID == "" {
		agentID = "samantha"
	}
	return agentID
}

func gatewaySessionKey(agentID, sessionID string) string {
	return fmt.Sprintf("agent:%s:%s", strings.TrimSpace(agentID), strings.TrimSpace(sessionID))
}

func (a *GatewayAdapter) getPooledConn(sessionKey string) *pooledGatewayConn {
	sessionKey = strings.TrimSpace(sessionKey)
	a.poolMu.Lock()
	defer a.poolMu.Unlock()
	pc := a.pool[sessionKey]
	if pc == nil {
		pc = &pooledGatewayConn{}
		a.pool[sessionKey] = pc
	}
	return pc
}

func (a *GatewayAdapter) dropPooledConn(sessionKey string, pc *pooledGatewayConn) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || pc == nil {
		return
	}
	a.poolMu.Lock()
	current := a.pool[sessionKey]
	if current == pc {
		delete(a.pool, sessionKey)
	}
	a.poolMu.Unlock()
	pc.Close()
}

func (a *GatewayAdapter) dialAndConnect(ctx context.Context) (*websocket.Conn, *gatewayWS, error) {
	conn, resp, err := a.dialer.DialContext(ctx, a.wsURL, nil)
	if err != nil {
		if resp != nil {
			return nil, nil, fmt.Errorf("openclaw gateway dial failed (%s): %w", resp.Status, err)
		}
		return nil, nil, fmt.Errorf("openclaw gateway dial failed: %w", err)
	}

	ws := newGatewayWS(conn)
	nonce, err := ws.readChallenge(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	connectID := uuid.NewString()
	scopes := []string{"operator.read", "operator.write"}
	signedAt := time.Now().UnixMilli()
	sigPayload := connectSignatureString(a.deviceID, a.clientID, a.clientMode, "operator", scopes, signedAt, a.token, nonce)
	sig := ed25519.Sign(a.privateKey, []byte(sigPayload))

	connectReq := gatewayRequest{
		Type:   "req",
		ID:     connectID,
		Method: "connect",
		Params: connectParams{
			MinProtocol: 3,
			MaxProtocol: 3,
			Client: connectClient{
				ID:       a.clientID,
				Version:  a.clientVersion,
				Platform: a.platform,
				Mode:     a.clientMode,
			},
			Role:        "operator",
			Scopes:      scopes,
			Caps:        []string{},
			Commands:    []string{},
			Permissions: map[string]any{},
			Auth:        &connectAuth{Token: a.token},
			Locale:      a.locale,
			UserAgent:   a.userAgent,
			Device: connectDevice{
				ID:        a.deviceID,
				PublicKey: a.publicKeyB64,
				Signature: base64.StdEncoding.EncodeToString(sig),
				SignedAt:  signedAt,
				Nonce:     nonce,
			},
		},
	}
	if err := writeGatewayJSON(conn, connectReq, gatewayConnectWriteTimeout); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("openclaw gateway connect write: %w", err)
	}
	if err := ws.waitForResponseOK(ctx, connectID); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ws, nil
}

func (a *GatewayAdapter) streamAgent(
	ctx context.Context,
	conn *websocket.Conn,
	ws *gatewayWS,
	prompt, agentID, sessionKey, runID string,
	onDelta DeltaHandler,
) (MessageResponse, bool, error) {
	agentReqID := uuid.NewString()
	agentReq := gatewayRequest{
		Type:   "req",
		ID:     agentReqID,
		Method: "agent",
		Params: agentParams{
			Message:        prompt,
			IdempotencyKey: runID,
			AgentID:        agentID,
			SessionKey:     sessionKey,
			Thinking:       a.thinking,
		},
	}
	if err := writeGatewayJSON(conn, agentReq, gatewayAgentWriteTimeout); err != nil {
		return MessageResponse{}, false, fmt.Errorf("openclaw gateway agent write: %w", err)
	}

	var (
		outSB              strings.Builder
		finalTextFromReply string
		collector          = newDeltaStreamCollector(a.streamMinChars)
		keepConn           = true
	)

streamLoop:
	for {
		frame, err := ws.nextFrame(ctx)
		if err != nil {
			keepConn = false
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = conn.Close()
				return MessageResponse{}, false, err
			}
			// Websocket close after final response is normal; prefer what we already collected.
			if finalTextFromReply != "" || outSB.Len() > 0 {
				break
			}
			return MessageResponse{}, false, err
		}

		switch frame.Type {
		case "event":
			if frame.Event != "agent" {
				continue
			}
			var evt agentEventPayload
			if err := json.Unmarshal(frame.Payload, &evt); err != nil {
				continue
			}
			if strings.TrimSpace(evt.RunID) != runID {
				continue
			}
			delta := gatewayEventDelta(evt.Stream, evt.Data)
			if strings.TrimSpace(delta) == "" {
				continue
			}
			if unseen := unseenSuffix(outSB.String(), delta); unseen != "" {
				delta = unseen
			} else {
				continue
			}
			outSB.WriteString(delta)
			if onDelta != nil {
				for _, seg := range collector.Consume(delta) {
					if err := onDelta(seg); err != nil {
						_ = conn.Close()
						return MessageResponse{}, false, err
					}
				}
			}
		case "res":
			if frame.ID != agentReqID {
				continue
			}
			if !frame.OK {
				msg := "openclaw gateway agent failed"
				if frame.Error != nil && strings.TrimSpace(frame.Error.Message) != "" {
					msg = frame.Error.Message
				}
				return MessageResponse{}, false, fmt.Errorf("%s", msg)
			}

			// The agent method responds twice: an early ack (status=accepted) and a final response.
			if gatewayIsAcceptedAgentResponse(frame.Payload) {
				continue
			}

			finalTextFromReply = gatewayFinalResponseText(frame.Payload)
			break streamLoop
		default:
			// ignore
		}
	}

	if onDelta != nil {
		for _, seg := range collector.Finalize() {
			if strings.TrimSpace(seg) == "" {
				continue
			}
			if err := onDelta(seg); err != nil {
				return MessageResponse{}, false, err
			}
		}
	}

	finalText := strings.TrimSpace(finalTextFromReply)
	if finalText == "" {
		finalText = strings.TrimSpace(outSB.String())
	}
	return MessageResponse{Text: finalText}, keepConn, nil
}

func gatewayAssistantDelta(data agentAssistantData) string {
	if strings.TrimSpace(data.Delta) != "" {
		return data.Delta
	}
	if strings.TrimSpace(data.Text) != "" {
		return data.Text
	}
	return ""
}

func gatewayEventDelta(stream string, payload json.RawMessage) string {
	if !gatewayStreamMayContainAssistantText(stream) {
		return ""
	}

	var data agentAssistantData
	if err := json.Unmarshal(payload, &data); err == nil {
		if delta := strings.TrimSpace(gatewayAssistantDelta(data)); delta != "" {
			return delta
		}
	}

	raw := strings.TrimSpace(string(payload))
	if raw == "" {
		return ""
	}
	if parsed := strings.TrimSpace(parseCLIReply(raw)); parsed != "" {
		return parsed
	}
	obj, ok := parseJSONObject(raw)
	if !ok {
		return ""
	}
	if text := pickStringField(obj, "delta", "text", "output", "message", "reply"); text != "" {
		return text
	}
	if nested, ok := obj["data"].(map[string]any); ok {
		if text := pickStringField(nested, "delta", "text", "output", "message", "reply"); text != "" {
			return text
		}
	}
	if nested, ok := obj["result"].(map[string]any); ok {
		if text := pickStringField(nested, "delta", "text", "output", "message", "reply"); text != "" {
			return text
		}
		if data, ok := nested["data"].(map[string]any); ok {
			if text := pickStringField(data, "delta", "text", "output", "message", "reply"); text != "" {
				return text
			}
		}
	}
	return ""
}

func gatewayStreamMayContainAssistantText(stream string) bool {
	stream = strings.ToLower(strings.TrimSpace(stream))
	if stream == "" {
		return false
	}
	if stream == "assistant" || stream == "output" || stream == "text" {
		return true
	}
	if strings.Contains(stream, "assistant") {
		return true
	}
	if strings.Contains(stream, "response") {
		return true
	}
	return false
}

func gatewayIsAcceptedAgentResponse(payload json.RawMessage) bool {
	raw := strings.TrimSpace(string(payload))
	if raw == "" {
		return false
	}
	obj, ok := parseJSONObject(raw)
	if !ok {
		return false
	}

	if strings.EqualFold(pickStringField(obj, "status"), "accepted") {
		return true
	}
	if accepted, ok := obj["accepted"].(bool); ok && accepted {
		return true
	}

	result, ok := obj["result"].(map[string]any)
	if !ok {
		return false
	}
	if strings.EqualFold(pickStringField(result, "status"), "accepted") {
		return true
	}
	if accepted, ok := result["accepted"].(bool); ok && accepted {
		return true
	}
	return false
}

func gatewayFinalResponseText(payload json.RawMessage) string {
	raw := strings.TrimSpace(string(payload))
	if raw == "" {
		return ""
	}

	if parsed := strings.TrimSpace(parseCLIReply(raw)); parsed != "" {
		return parsed
	}

	obj, ok := parseJSONObject(raw)
	if !ok {
		return ""
	}
	if text := pickStringField(obj, "text", "output", "message", "reply"); text != "" {
		return text
	}

	if data, ok := obj["data"].(map[string]any); ok {
		if text := pickStringField(data, "text", "output", "message", "reply"); text != "" {
			return text
		}
	}

	if result, ok := obj["result"].(map[string]any); ok {
		if text := pickStringField(result, "text", "output", "message", "reply"); text != "" {
			return text
		}
		if data, ok := result["data"].(map[string]any); ok {
			if text := pickStringField(data, "text", "output", "message", "reply"); text != "" {
				return text
			}
		}
	}

	return ""
}

type gatewayWS struct {
	conn *websocket.Conn
	msgs chan []byte
	errs chan error
}

func newGatewayWS(conn *websocket.Conn) *gatewayWS {
	ws := &gatewayWS{
		conn: conn,
		msgs: make(chan []byte, 256),
		errs: make(chan error, 1),
	}
	go func() {
		defer close(ws.msgs)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				ws.errs <- err
				return
			}
			ws.msgs <- data
		}
	}()
	return ws
}

func (ws *gatewayWS) nextFrame(ctx context.Context) (gatewayFrame, error) {
	data, err := ws.nextMessage(ctx)
	if err != nil {
		return gatewayFrame{}, err
	}
	var frame gatewayFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return gatewayFrame{}, fmt.Errorf("openclaw gateway frame parse: %w", err)
	}
	return frame, nil
}

func (ws *gatewayWS) nextMessage(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-ws.errs:
		if err == nil {
			return nil, ioErrEOF()
		}
		return nil, err
	case data, ok := <-ws.msgs:
		if !ok {
			select {
			case err := <-ws.errs:
				if err != nil {
					return nil, err
				}
			default:
			}
			return nil, ioErrEOF()
		}
		return data, nil
	}
}

func ioErrEOF() error { return errors.New("openclaw gateway connection closed") }

func (ws *gatewayWS) readChallenge(ctx context.Context) (string, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if time.Now().After(deadline) {
			return "", errors.New("openclaw gateway connect challenge timeout")
		}
		frame, err := ws.nextFrame(ctx)
		if err != nil {
			return "", err
		}
		if frame.Type != "event" || frame.Event != "connect.challenge" {
			continue
		}
		var payload connectChallengePayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			continue
		}
		nonce := strings.TrimSpace(payload.Nonce)
		if nonce == "" {
			continue
		}
		return nonce, nil
	}
}

func (ws *gatewayWS) waitForResponseOK(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("openclaw gateway response id missing")
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("openclaw gateway response timeout (id=%s)", id)
		}
		frame, err := ws.nextFrame(ctx)
		if err != nil {
			return err
		}
		if frame.Type != "res" || frame.ID != id {
			continue
		}
		if frame.OK {
			return nil
		}
		msg := "openclaw gateway request failed"
		if frame.Error != nil {
			if strings.TrimSpace(frame.Error.Message) != "" {
				msg = frame.Error.Message
			} else if strings.TrimSpace(frame.Error.Code) != "" {
				msg = frame.Error.Code
			}
		}
		return fmt.Errorf("%s", msg)
	}
}

func writeGatewayJSON(conn *websocket.Conn, payload any, timeout time.Duration) error {
	if conn == nil {
		return errors.New("openclaw gateway connection is nil")
	}
	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		defer conn.SetWriteDeadline(time.Time{})
	}
	return conn.WriteJSON(payload)
}
