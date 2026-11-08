// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package overlay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/gofiber/fiber/v3"
)

const validScene = `{"resolution":"1080p","layers":[{"id":"text","name":"Text","kind":"text","x":5,"y":5,"w":80,"h":20,"opacity":100,"content":"<script>alert(1)</script>","fontSize":7,"color":"#ffffff","align":"start"}]}`

func newTestServer(t *testing.T, dir string) *Server {
	t.Helper()
	source, err := OpenSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(source, fstest.MapFS{"index.html": {Data: []byte("renderer")}, "assets/main.js": {Data: []byte("// isolated")}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(t *testing.T, s *Server, method, path, token string, headers map[string]string) *http.Response {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+Address+path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		if k == "Host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	response, err := s.app.Test(r, fiber.TestConfig{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func TestReadOnlyBoundary(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	token := strings.Split(s.source.SourceURL(), "#")[1]
	for _, tc := range []struct {
		name, method, path, token string
		headers                   map[string]string
		status                    int
	}{
		{"public renderer", "GET", "/", "", nil, 200},
		{"javascript", "GET", "/assets/main.js", "", nil, 200},
		{"missing credential", "GET", "/state", "", nil, 401},
		{"incorrect credential", "GET", "/state", "wrong", nil, 401},
		{"authorized snapshot", "GET", "/state", token, nil, 200},
		{"write blocked", "POST", "/state", token, nil, 405},
		{"DNS rebinding", "GET", "/state", token, map[string]string{"Host": "attacker.example:18947"}, 403},
		{"foreign origin", "GET", "/state", token, map[string]string{"Origin": "https://attacker.example"}, 403},
		{"cross site navigation", "GET", "/", "", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"no desktop bridge", "GET", "/wails/runtime.js", "", nil, 404},
		{"no config file", "GET", "/overlay.json", "", nil, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t, s, tc.method, tc.path, tc.token, tc.headers)
			if r.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", r.StatusCode, tc.status)
			}
			if r.Header.Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("unexpected CORS")
			}
			if r.Header.Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
			if tc.name == "javascript" && !strings.Contains(r.Header.Get("Content-Type"), "javascript") {
				t.Fatalf("incorrect MIME: %s", r.Header.Get("Content-Type"))
			}
		})
	}
}
func TestPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(t, dir)
	url := s.source.SourceURL()
	if err := s.source.Publish(validScene); err != nil {
		t.Fatal(err)
	}
	restarted := newTestServer(t, dir)
	if restarted.source.SourceURL() != url || len(restarted.source.Current().Layers) != 1 {
		t.Fatal("source URL and scene must survive restart")
	}
	info, err := os.Stat(filepath.Join(dir, "overlay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential file permissions %o", info.Mode().Perm())
	}
	for _, raw := range []string{
		strings.Replace(validScene, `"x":5`, `"x":99`, 1),
		strings.Replace(validScene, `"kind":"text"`, `"kind":"html"`, 1),
		strings.Replace(validScene, `"#ffffff"`, `"url(https://attacker.example)"`, 1),
		strings.Replace(validScene, `"1080p"`, `"4k"`, 1),
		strings.Repeat("x", 256*1024+1),
	} {
		if err := s.source.Publish(raw); err == nil {
			t.Fatal("accepted invalid scene")
		}
	}
	if s.source.current.Load().revision != 2 {
		t.Fatal("failed publications must not change revision")
	}
}
func TestPublishWakesReaders(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	token := strings.Split(s.source.SourceURL(), "#")[1]
	// Warm Fiber's router before making requests concurrently.
	request(t, s, "GET", "/", "", nil)
	result := make(chan Snapshot, 1)
	errs := make(chan error, 1)
	go func() {
		r := httptest.NewRequest("GET", "http://"+Address+"/state?after=1", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		res, err := s.app.Test(r, fiber.TestConfig{Timeout: 2 * time.Second})
		if err != nil {
			errs <- err
			return
		}
		defer res.Body.Close()
		var snapshot Snapshot
		err = codec.NewDecoder(res.Body).Decode(&snapshot)
		if err != nil {
			errs <- err
			return
		}
		result <- snapshot
	}()
	waitForReader(t, s)
	if err := s.source.Publish(validScene); err != nil {
		t.Fatal(err)
	}
	select {
	case snapshot := <-result:
		if snapshot.Revision != 2 || len(snapshot.Scene.Layers) != 1 {
			t.Fatal("stale snapshot")
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("publication did not wake reader")
	}
}

func TestReaderLimit(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	token := strings.Split(s.source.SourceURL(), "#")[1]
	// Full slots reject new readers immediately instead of retaining unbounded waiters.
	for range cap(s.slots) {
		s.slots <- struct{}{}
	}
	if r := request(t, s, "GET", "/state", token, nil); r.StatusCode != 429 {
		body, _ := io.ReadAll(r.Body)
		t.Fatalf("expected limit, got %d %s", r.StatusCode, body)
	}
}

func TestResetRevokesWaitingReader(t *testing.T) {
	s := newTestServer(t, t.TempDir())
	old := s.source.SourceURL()
	token := strings.Split(old, "#")[1]
	request(t, s, "GET", "/", "", nil)
	result := make(chan int, 1)
	go func() {
		r := httptest.NewRequest("GET", "http://"+Address+"/state?after=1", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		response, err := s.app.Test(r, fiber.TestConfig{Timeout: 2 * time.Second})
		if err != nil {
			result <- 0
			return
		}
		response.Body.Close()
		result <- response.StatusCode
	}()
	waitForReader(t, s)
	next, err := s.source.ResetSourceURL()
	if err != nil {
		t.Fatal(err)
	}
	if next == old {
		t.Fatal("reset did not replace credential")
	}
	select {
	case status := <-result:
		if status != 401 {
			t.Fatalf("revoked waiter got %d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked waiter did not wake")
	}
	if r := request(t, s, "GET", "/state", token, nil); r.StatusCode != 401 {
		t.Fatal("old token still accepted")
	}
}

func waitForReader(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(s.slots) == 0 {
		select {
		case <-deadline:
			t.Fatal("reader did not connect")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
