package neko

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func allocation(first uint16) map[uint16]uint16 {
	m := map[uint16]uint16{}
	for n := 0; n < 20; n++ {
		m[60020+uint16(n)] = first + uint16(n)
	}
	return m
}

// TestMediaAllocationValidationAndImmutability verifies media allocation validation and immutability.
func TestMediaAllocationValidationAndImmutability(t *testing.T) {
	c, _ := connected(t)
	for _, ports := range []map[uint16]uint16{nil, {60020: 1}} {
		if c.SetMediaPorts(ports) == nil {
			t.Fatal("incomplete allocation")
		}
	}
	bad := allocation(45000)
	delete(bad, 60039)
	bad[60040] = 45019
	if c.SetMediaPorts(bad) == nil {
		t.Fatal("wrong guest range")
	}
	bad = allocation(45000)
	bad[60020] = 0
	if c.SetMediaPorts(bad) == nil {
		t.Fatal("zero host")
	}
	bad = allocation(45000)
	bad[60020] = bad[60021]
	if c.SetMediaPorts(bad) == nil {
		t.Fatal("duplicate host")
	}
	ports := allocation(45000)
	if err := c.SetMediaPorts(ports); err != nil {
		t.Fatal(err)
	}
	ports[60020] = 1
	c.mu.Lock()
	stored := c.mediaPorts[60020]
	c.mu.Unlock()
	if stored != 45000 {
		t.Fatal("caller mutated stored allocation")
	}
	if _, err := c.CreateViewer(context.Background(), "active"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMediaPorts(allocation(45000)); err != nil {
		t.Fatal("identical map not idempotent", err)
	}
	if err := c.SetMediaPorts(allocation(46000)); err == nil {
		t.Fatal("active allocation changed")
	}
}

// TestSignalRewriteOnlyExactCandidatePortFields verifies signal rewrite only exact candidate port fields.
func TestSignalRewriteOnlyExactCandidatePortFields(t *testing.T) {
	ports := allocation(45000)
	sdp := "v=0\r\ns=Session candidate:label\r\nc=IN IP4 0.0.0.0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtcp:60020 IN IP4 127.0.0.1\r\na=candidate:one 1 udp 12345 127.0.0.1 60020 typ host generation 0 ufrag 60020\r\na=candidate:two\t1 UDP 333 127.0.0.1 60039 typ srflx raddr 0.0.0.0 rport 0\r\na=candidate:tcp 1 tcp 123 127.0.0.1 60020 typ host tcptype passive\r\na=end-of-candidates\r\n"
	raw, _ := json.Marshal(map[string]any{"event": "signal/provide", "payload": map[string]any{"sdp": sdp, "iceservers": []map[string]string{{"urls": "stun:127.0.0.1:9"}}, "unrelated": "candidate:x 1 udp 1 127.0.0.1 60020 typ host"}, "other": map[string]any{"port": 60020}})
	rewritten, err := rewriteSignalPorts(raw, ports)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Payload struct {
			SDP       string `json:"sdp"`
			Unrelated string `json:"unrelated"`
		}
		Other struct {
			Port int `json:"port"`
		}
	}
	if json.Unmarshal(rewritten, &envelope) != nil {
		t.Fatal("invalid rewritten JSON")
	}
	expected := strings.Replace(sdp, "127.0.0.1 60020 typ host generation", "127.0.0.1 45000 typ host generation", 1)
	expected = strings.Replace(expected, "127.0.0.1 60039 typ srflx", "127.0.0.1 45019 typ srflx", 1)
	if envelope.Payload.SDP != expected || envelope.Other.Port != 60020 || !strings.Contains(envelope.Payload.Unrelated, "60020") {
		t.Fatalf("noncandidate fields changed: %s", rewritten)
	}
	unrelated := []byte(" {\"event\":\"system/init\", \"payload\":{\"candidate\":\"not a candidate\"}} ")
	actual, err := rewriteSignalPorts(unrelated, ports)
	if err != nil || !bytes.Equal(actual, unrelated) {
		t.Fatal("unrelated message bytes changed", err)
	}
	for _, end := range []string{`""`, `null`} {
		b := []byte(`{"event":"signal/candidate","payload":{"candidate":` + end + `}}`)
		actual, err = rewriteSignalPorts(b, ports)
		if err != nil || !bytes.Equal(actual, b) {
			t.Fatal("end candidate changed", err)
		}
	}
}

// TestSignalMalformedAndUnexpectedDestinationsFailClosed verifies signal malformed and unexpected destinations fail closed.
func TestSignalMalformedAndUnexpectedDestinationsFailClosed(t *testing.T) {
	ports := allocation(45000)
	for _, candidate := range []string{
		"candidate: 1 udp 1 127.0.0.1 60020 typ host", "candidate:x 0 udp 1 127.0.0.1 60020 typ host", "candidate:x 1 udp bad 127.0.0.1 60020 typ host", "candidate:x 1 udp 1 127.0.0.1 0 typ host", "candidate:x 1 udp 1 127.0.0.1 65536 typ host", "candidate:x 1 udp 1 127.0.0.1 60019 typ host", "candidate:x 1 udp 1 172.17.0.2 60020 typ host", "candidate:x 1 udp 1 ::1 60020 typ host", "candidate:x 1 udp 1 127.0.0.1 60020 what host", "candidate:x 1 udp 1 127.0.0.1 60020 typ evil", "candidate:x 1 sctp 1 127.0.0.1 60020 typ host", "candidate:x 1 udp 1 127.0.0.1 60020 typ host\r\na=evil", " candidate:x 1 udp 1 127.0.0.1 60020 typ host", strings.Repeat("x", 4097),
	} {
		if _, err := rewriteCandidate(candidate, ports); err == nil {
			t.Fatal("accepted malformed candidate", candidate)
		}
	}
	for _, raw := range []string{`not JSON`, `{}`, `{"event":"signal/provide","payload":[]}`, `{"event":"signal/provide","payload":{"sdp":42}}`, `{"event":"signal/provide","payload":{}}`, `{"event":"signal/provide","payload":{"sdp":""}}`, `{"event":"signal/candidate","payload":{}}`, `{"event":"signal/candidate","payload":{"candidate":42}}`} {
		if _, err := rewriteSignalPorts([]byte(raw), ports); err == nil {
			t.Fatal("accepted bad signal", raw)
		}
	}
	if _, err := rewriteSignalPorts(bytes.Repeat([]byte{'x'}, maxMessage+1), ports); err == nil {
		t.Fatal("oversized message")
	}
	if _, err := rewriteSDP(strings.Repeat("x", 256*1024+1), ports); err == nil {
		t.Fatal("oversized SDP")
	}
	if _, err := rewriteSDP("v=0\r\n a=candidate:x 1 udp 1 127.0.0.1 60020 typ host\r\n", ports); err == nil {
		t.Fatal("malformed SDP candidate prefix")
	}
}

// TestRelayRewritesOnlyServerCandidates verifies relay rewrites only server candidates.
func TestRelayRewritesOnlyServerCandidates(t *testing.T) {
	c, f := connected(t)
	if err := c.SetMediaPorts(allocation(45000)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateViewer(context.Background(), "mapped"); err != nil {
		t.Fatal(err)
	}
	ws, result := viewerConnection(t, c, "mapped", nil)
	defer ws.CloseNow()
	candidate := []byte(`{"event":"signal/candidate","payload":{"candidate":"candidate:x 1 udp 1 127.0.0.1 60020 typ host","sdpMid":"0","sdpMLineIndex":0}}`)
	f.mu.Lock()
	upstream := f.viewer
	f.mu.Unlock()
	if err := upstream.Write(context.Background(), websocket.MessageText, candidate); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("127.0.0.1 45000 typ host")) {
		t.Fatal("server port not mapped", string(data))
	}
	if err = ws.Write(ctx, websocket.MessageText, candidate); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		return f.count() == 1
	})
	if !bytes.Contains(f.events()[0].Payload, []byte("127.0.0.1 60020 typ host")) {
		t.Fatal("client candidate was rewritten")
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
	select {
	case <-result:
	case <-ctx.Done():
		t.Fatal("relay failed cleanup")
	}
}

// TestMediaHandlerAndExportedTicketBinding verifies media handler and exported ticket binding.
func TestMediaHandlerAndExportedTicketBinding(t *testing.T) {
	h, _ := handlerSetup(t, false)
	ports := allocation(45000)
	rows := make([]map[string]uint16, 0, 20)
	for guest, host := range ports {
		rows = append(rows, map[string]uint16{"guest": guest, "host": host})
	}
	body, _ := json.Marshal(map[string]any{"ports": rows})
	w := handlerCall(t, h, "POST", "/desktop/media-ports", string(body), nil)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	w = handlerCall(t, h, "POST", "/desktop/media-ports", `{"ports":[{"guest":60020,"host":45000},{"guest":60020,"host":45001}]}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code)
	}
	ticket, err := h.CreateSDKViewer(context.Background(), "public-session")
	if err != nil {
		t.Fatal(err)
	}
	if !h.TicketBelongs(ticket.Token, "public-session") || h.TicketBelongs(ticket.Token, "other") || h.TicketBelongs("wrong", "public-session") {
		t.Fatal("ticket session binding")
	}
	if err = h.SetMediaPorts(allocation(46000)); err == nil {
		t.Fatal("changed active map")
	}
	h.mu.Lock()
	v := h.tickets[ticket.Token]
	v.Expires = time.Now().Add(-time.Second)
	h.tickets[ticket.Token] = v
	h.mu.Unlock()
	if h.TicketBelongs(ticket.Token, "public-session") {
		t.Fatal("expired ticket accepted")
	}
}
