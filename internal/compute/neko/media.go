package neko

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const mediaFirst uint16 = 60020
const mediaCount = 20

// SetMediaPorts configures the trusted Docker loopback UDP mapping. The complete
// allocation must come from inspected runtime endpoints, never an SDP or viewer.
// Mapping changes while a viewer exists are rejected; identical configuration is
// idempotent. The upstream server must advertise NAT1to1=127.0.0.1.
func (c *Client) SetMediaPorts(ports map[uint16]uint16) error {
	if len(ports) != mediaCount {
		return errors.New("neko: exactly twenty media ports are required")
	}
	copyPorts := make(map[uint16]uint16, mediaCount)
	hosts := map[uint16]bool{}
	for n := range mediaCount {
		guest := mediaFirst + uint16(n)
		host, ok := ports[guest]
		if !ok || host == 0 || hosts[host] {
			return errors.New("neko: invalid or duplicate media port allocation")
		}
		copyPorts[guest] = host
		hosts[host] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	same := len(c.mediaPorts) == len(copyPorts)
	if same {
		for guest, host := range copyPorts {
			if c.mediaPorts[guest] != host {
				same = false
				break
			}
		}
	}
	if len(c.viewers) > 0 && !same {
		return errors.New("neko: media allocation is immutable while viewers exist")
	}
	c.mediaPorts = copyPorts
	return nil
}

// SetMediaPorts updates the approved media port mappings.
func (h *Handler) SetMediaPorts(ports map[uint16]uint16) error {
	return h.client.SetMediaPorts(ports)
}

// rewriteSignal transforms only server-generated candidate data in the exact
// Neko v3 signaling payloads. Unknown messages and unchanged data retain their
// original bytes; this is not a recursive substitution over arbitrary JSON.
func (c *Client) rewriteSignal(data []byte) ([]byte, error) {
	c.mu.Lock()
	ports := make(map[uint16]uint16, len(c.mediaPorts))
	for guest, host := range c.mediaPorts {
		ports[guest] = host
	}
	c.mu.Unlock()
	return rewriteSignalPorts(data, ports)
}
func rewriteSignalPorts(data []byte, ports map[uint16]uint16) ([]byte, error) {
	if len(data) > maxMessage {
		return nil, errors.New("neko: signal exceeds limit")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil {
		return nil, errors.New("neko: invalid signaling json")
	}
	var event string
	if json.Unmarshal(envelope["event"], &event) != nil {
		return nil, errors.New("neko: invalid signaling event")
	}
	switch event {
	case "signal/provide", "signal/offer", "signal/answer", "signal/candidate":
	default:
		return data, nil
	}
	if len(ports) != mediaCount {
		return nil, errors.New("neko: complete media allocation required")
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(envelope["payload"], &payload) != nil || payload == nil {
		return nil, errors.New("neko: invalid signaling payload")
	}
	if event != "signal/candidate" {
		if _, exists := payload["sdp"]; !exists {
			return nil, errors.New("neko: sdp field required")
		}
	}
	changed := false
	if raw, exists := payload["sdp"]; exists {
		var sdp string
		if json.Unmarshal(raw, &sdp) != nil || sdp == "" {
			return nil, errors.New("neko: invalid sdp field")
		}
		rewritten, err := rewriteSDP(sdp, ports)
		if err != nil {
			return nil, err
		}
		if rewritten != sdp {
			payload["sdp"], _ = json.Marshal(rewritten)
			changed = true
		}
	}
	if event == "signal/candidate" {
		raw, exists := payload["candidate"]
		if !exists {
			return nil, errors.New("neko: candidate field required")
		}
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var candidate string
			if json.Unmarshal(raw, &candidate) != nil {
				return nil, errors.New("neko: invalid candidate field")
			}
			rewritten, err := rewriteCandidate(candidate, ports)
			if err != nil {
				return nil, err
			}
			if rewritten != candidate {
				payload["candidate"], _ = json.Marshal(rewritten)
				changed = true
			}
		}
	}
	if !changed {
		return data, nil
	}
	envelope["payload"], _ = json.Marshal(payload)
	rewritten, err := json.Marshal(envelope)
	if err != nil {
		return nil, errors.New("neko: signaling encoding failed")
	}
	return rewritten, nil
}
func rewriteSDP(sdp string, ports map[uint16]uint16) (string, error) {
	if len(sdp) > 256*1024 {
		return "", errors.New("neko: sdp exceeds limit")
	}
	lines := strings.SplitAfter(sdp, "\n")
	for i, line := range lines {
		content := strings.TrimSuffix(line, "\n")
		ending := line[len(content):]
		if strings.HasSuffix(content, "\r") {
			content = strings.TrimSuffix(content, "\r")
			ending = "\r" + ending
		}
		if !strings.HasPrefix(content, "a=candidate:") {
			if strings.HasPrefix(strings.TrimSpace(content), "a=candidate:") || strings.HasPrefix(content, "candidate:") {
				return "", errors.New("neko: malformed sdp candidate line")
			}
			continue
		}
		rewritten, err := rewriteCandidate(strings.TrimPrefix(content, "a="), ports)
		if err != nil {
			return "", err
		}
		lines[i] = "a=" + rewritten + ending
	}
	return strings.Join(lines, ""), nil
}

type candidateField struct{ start, end int }

func candidateFields(candidate string) ([]candidateField, error) {
	if len(candidate) > 4096 {
		return nil, errors.New("neko: candidate exceeds limit")
	}
	var fields []candidateField
	start := -1
	for i, ch := range []byte(candidate) {
		if ch == ' ' || ch == '\t' {
			if start >= 0 {
				fields = append(fields, candidateField{start, i})
				start = -1
			}
			continue
		}
		if ch < 33 || ch > 126 {
			return nil, errors.New("neko: invalid candidate character")
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, candidateField{start, len(candidate)})
	}
	return fields, nil
}
func rewriteCandidate(candidate string, ports map[uint16]uint16) (string, error) {
	if candidate == "" {
		return candidate, nil
	} // This marks the end of candidates.
	fields, err := candidateFields(candidate)
	if err != nil {
		return "", err
	}
	if len(fields) < 8 {
		return "", errors.New("neko: incomplete ice candidate")
	}
	field := func(n int) string {
		return candidate[fields[n].start:fields[n].end]
	}
	foundation := strings.TrimPrefix(field(0), "candidate:")
	if fields[0].start != 0 || !strings.HasPrefix(field(0), "candidate:") || len(foundation) == 0 || len(foundation) > 128 {
		return "", errors.New("neko: invalid candidate foundation")
	}
	component, err := strconv.ParseUint(field(1), 10, 16)
	if err != nil || component < 1 || component > 2 {
		return "", errors.New("neko: invalid candidate component")
	}
	if _, err = strconv.ParseUint(field(3), 10, 32); err != nil {
		return "", errors.New("neko: invalid candidate priority")
	}
	guest, err := strconv.ParseUint(field(5), 10, 16)
	if err != nil || guest == 0 || field(6) != "typ" {
		return "", errors.New("neko: invalid candidate port or type marker")
	}
	switch field(7) {
	case "host", "srflx", "prflx", "relay":
	default:
		return "", errors.New("neko: invalid candidate type")
	}
	switch strings.ToLower(field(2)) {
	case "udp":
	default:
		return "", errors.New("neko: unsupported candidate transport")
	}
	host, ok := ports[uint16(guest)]
	if field(4) != "127.0.0.1" || !ok || uint16(guest) < mediaFirst || uint16(guest) >= mediaFirst+mediaCount || host == 0 {
		return "", errors.New("neko: candidate does not match the trusted loopback media allocation")
	}
	// Replace only the precise UDP port field. All other candidate bytes, including
	// foundation, spacing, related-address hints and extension values, are retained.
	return candidate[:fields[5].start] + fmt.Sprint(host) + candidate[fields[5].end:], nil
}
