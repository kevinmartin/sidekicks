//go:build integration

package browseruse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/stretchr/testify/require"
)

func testCLIViewer(t *testing.T, binary, backend string) {
	t.Helper()
	name := testInstanceName()
	registerCLICleanup(t, binary, backend, name)
	browser := func(t *testing.T, args ...string) []byte {
		t.Helper()
		return callCLI(t, binary, append([]string{"--runtime", backend, "browser", "--name", name}, args...)...)
	}
	browser(t, "start")
	var tabs browserapi.Tabs
	require.NoError(t, json.Unmarshal(browser(t, "tabs"), &tabs))
	var popup struct {
		ID string `json:"targetId"`
	}
	require.NoError(t, json.Unmarshal(browser(t, "cdp", "Target.createTarget", `{"url":"about:blank"}`), &popup))
	require.NotEmpty(t, popup.ID)

	const form = `<style>body{background:#902414;min-height:300vh}input,button{position:fixed;left:10%;width:24%;height:7%}input{top:15%}button{top:34%}</style>
<input id="entry" aria-label="Message"><button onclick="document.querySelector('#result').textContent=document.querySelector('#entry').value;document.body.style.background='#205cbd'">Submit</button><p id="result"></p>`
	for _, tab := range []string{tabs.Primary, popup.ID} {
		browser(t, "cmd", "--tab", tab, "goto", "data:text/html,"+url.PathEscape(form))
	}
	for _, fixture := range []struct {
		name, tab, other, text string
		flags                  []string
	}{
		{
			name:  "primary tab",
			tab:   tabs.Primary,
			other: popup.ID,
			text:  "Agent handoff",
		},
		{
			name:  "explicit tab",
			tab:   popup.ID,
			other: tabs.Primary,
			text:  "Another handoff",
			flags: []string{"--tab", popup.ID},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			browser(t, "cdp", "--tab", fixture.tab, "Page.bringToFront")
			args := []string{"--runtime", backend, "browser", "view", "--name", name, "--no-open"}
			address := runViewerCLI(t, binary, append(args, fixture.flags...)...)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			verifyViewerAssets(t, ctx, address)
			socket := openViewer(t, ctx, address)
			defer func() {
				_ = socket.CloseNow()
			}()
			waitViewerColor(t, ctx, socket, false)

			browser(t, "cdp", "--tab", fixture.other, "Page.bringToFront")
			require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
				Type:   "wheel",
				X:      .75,
				Y:      .50,
				DeltaY: 240,
			}))
			clickViewer(t, ctx, socket, .18, .18)
			require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
				Type:    "keyDown",
				Key:     "A",
				Code:    "KeyA",
				KeyCode: 65,
				Text:    "A",
			}))
			require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
				Type:    "keyUp",
				Key:     "A",
				Code:    "KeyA",
				KeyCode: 65,
			}))
			require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
				Type: "text",
				Text: fixture.text[1:],
			}))
			clickViewer(t, ctx, socket, .18, .37)
			waitViewerColor(t, ctx, socket, true)

			data := browser(t, "cdp", "--tab", fixture.tab, "Runtime.evaluate",
				`{"expression":"document.getElementById('result').textContent + ':' + (scrollY > 0)","returnByValue":true}`)
			var result struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(data, &result))
			require.Equal(t, fixture.text+":true", result.Result.Value)
			require.Contains(t, string(browser(t, "cmd", "--tab", fixture.tab, "snapshot")), fixture.text)

			if fixture.tab == popup.ID {
				require.Contains(t, string(browser(t, "cmd", "snapshot")), "Agent handoff")
				browser(t, "cdp", "Target.closeTarget", `{"targetId":"`+popup.ID+`"}`)
				for {
					_, _, err := socket.Read(ctx)
					if err != nil {
						require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
						require.ErrorContains(t, err, "selected browser tab is closed")
						break
					}
				}
			}
		})
	}
	assertInstanceRunning(t, binary, backend, name, true)
}

func runViewerCLI(t *testing.T, binary string, args ...string) *url.URL {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(ctx, binary, args...)
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		if err := command.Process.Signal(os.Interrupt); err != nil {
			cancel()
		}
		waited := make(chan error, 1)
		go func() {
			waited <- command.Wait()
		}()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case err := <-waited:
			require.NoError(t, err)
		case <-timer.C:
			cancel()
			<-waited
			t.Error("viewer CLI did not terminate on interrupt")
		}
		cancel()
	})
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Scan()
		line <- scanner.Text()
	}()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case raw := <-line:
		address, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", address.Hostname())
		require.NotEmpty(t, address.Fragment)
		return address
	case <-timer.C:
		t.Fatal("viewer CLI did not print a URL")
		return nil
	}
}

func verifyViewerAssets(t *testing.T, ctx context.Context, address *url.URL) {
	t.Helper()
	for path, want := range map[string]string{
		"/":           "Sidekicks Browser Viewer",
		"/viewer.js":  "new WebSocket",
		"/viewer.css": "#screen",
	} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address.Host+path, nil)
		require.NoError(t, err)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, string(data), want)
	}
}

func openViewer(t *testing.T, ctx context.Context, address *url.URL) *websocket.Conn {
	t.Helper()
	options := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": {"http://" + address.Host},
		},
		Subprotocols: []string{browserapi.ViewerProtocol, address.Fragment},
	}
	socket, _, err := websocket.Dial(ctx, "ws://"+address.Host+"/stream", options)
	require.NoError(t, err)
	require.Equal(t, browserapi.ViewerProtocol, socket.Subprotocol())
	socket.SetReadLimit(browserapi.ViewerFrameLimit)
	return socket
}

func clickViewer(t *testing.T, ctx context.Context, socket *websocket.Conn, x, y float64) {
	t.Helper()
	require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
		Type:       "pointerDown",
		Button:     "left",
		Buttons:    1,
		ClickCount: 1,
		X:          x,
		Y:          y,
	}))
	require.NoError(t, wsjson.Write(ctx, socket, browserapi.ViewerInput{
		Type:       "pointerUp",
		Button:     "left",
		ClickCount: 1,
		X:          x,
		Y:          y,
	}))
}

func waitViewerColor(t *testing.T, ctx context.Context, socket *websocket.Conn, blue bool) {
	t.Helper()
	for {
		var frame browserapi.ViewerFrame
		require.NoError(t, wsjson.Read(ctx, socket, &frame))
		data, err := base64.StdEncoding.DecodeString(frame.JPEG)
		require.NoError(t, err)
		image, err := jpeg.Decode(bytes.NewReader(data))
		require.NoError(t, err)
		bounds := image.Bounds()
		r, _, b, _ := image.At(bounds.Max.X*9/10, bounds.Max.Y*9/10).RGBA()
		if (blue && b > 2*r) || (!blue && r > 2*b) {
			return
		}
	}
}
