package onboarding

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEncodeTOS verifies the TOS gRPC-web frame matches the reference byte layout:
// grpc-web header 00 00 00 00 02 + payload field2(bool true) 10 01.
func TestEncodeTOS(t *testing.T) {
	got := grpcWebFrame([]byte{0x10, 0x01})
	want, _ := hex.DecodeString("00000000021001")
	if !bytes.Equal(got, want) {
		t.Fatalf("TOS frame = %s, want %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
}

// TestEncodeNSFW verifies the NSFW gRPC-web frame matches the reference layout.
func TestEncodeNSFW(t *testing.T) {
	got := encodeNSFW()
	// field1 = 0A 02 10 01  (4 bytes)
	// field2 = 12 <len> 0A <len> "always_show_nsfw_content"
	// flag is 24 bytes; field2_inner = 1(tag)+1(len)+24 = 26 = 0x1a
	// field2 = 1(tag)+1(len)+26 = 28 ; payload = 4 + 28 = 32 = 0x20
	want, _ := hex.DecodeString("0000000020" + "0a021001" + "12" + "1a" + "0a" + "18" +
		hex.EncodeToString([]byte(nsfwFlag)))
	if !bytes.Equal(got, want) {
		t.Fatalf("NSFW frame = %s, want %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
	if l := len(got); l != 37 { // 5 header + 4 field1 + 2 + 26 field2
		t.Fatalf("NSFW frame length = %d, want 37", l)
	}
}

// TestRandomBirthDateAgeRange checks the generated birth date is 20-40 years old.
func TestRandomBirthDateAgeRange(t *testing.T) {
	now := time.Now().UTC()
	for i := 0; i < 200; i++ {
		s := randomBirthDate()
		ts, err := time.Parse("2006-01-02T15:04:05.000Z", s)
		if err != nil {
			t.Fatalf("parse birth date %q: %v", s, err)
		}
		age := now.Year() - ts.Year()
		if ts.AddDate(age, 0, 0).After(now) {
			age--
		}
		if age < 20 || age > 40 {
			t.Fatalf("birth date %q age %d out of [20,40]", s, age)
		}
	}
}

func TestBuildCookie(t *testing.T) {
	if got := buildCookie("tok", ""); got != "sso=tok; sso-rw=tok" {
		t.Fatalf("cookie without clearance = %q", got)
	}
	if got := buildCookie("tok", "cf"); got != "sso=tok; sso-rw=tok; cf_clearance=cf" {
		t.Fatalf("cookie with clearance = %q", got)
	}
}

// TestRunAllStepsSucceed drives Run against a fake server and asserts each
// endpoint is hit in order with the expected method, headers, and payload shape.
func TestRunAllStepsSucceed(t *testing.T) {
	var (
		mu       sync.Mutex
		order    []string
		tosBody  []byte
		nsfwBody []byte
		bdayBody []byte
		cookie   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		cookie = r.Header.Get("Cookie")
		mu.Unlock()
		switch r.URL.Path {
		case "/tos":
			mu.Lock()
			order = append(order, "tos")
			tosBody = body
			mu.Unlock()
			checkGRPCHeaders(t, r, "application/grpc-web+proto", "https://accounts.x.ai")
		case "/bday":
			mu.Lock()
			order = append(order, "bday")
			bdayBody = body
			mu.Unlock()
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("birth-date content-type = %q, want application/json", ct)
			}
		case "/nsfw":
			mu.Lock()
			order = append(order, "nsfw")
			nsfwBody = body
			mu.Unlock()
			checkGRPCHeaders(t, r, "application/grpc-web+proto", "https://grok.com")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Override the package URLs to point at the test server.
	tosURL, bdayURL, nsfwURL = srv.URL + "/tos", srv.URL + "/bday", srv.URL + "/nsfw"

	res, err := Run(context.Background(), "SSOTOKEN", "CFCLEAR", Options{Timeout: 5 * time.Second}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.TOS || !res.BirthDate || !res.NSFW {
		t.Fatalf("expected all steps true, got %+v", res)
	}

	mu.Lock()
	defer mu.Unlock()
	if got, want := strings.Join(order, ","), "tos,bday,nsfw"; got != want {
		t.Fatalf("step order = %q, want %q", got, want)
	}
	if want, _ := hex.DecodeString("00000000021001"); !bytes.Equal(tosBody, want) {
		t.Fatalf("TOS body = %s, want %s", hex.EncodeToString(tosBody), hex.EncodeToString(want))
	}
	if !bytes.Equal(nsfwBody, encodeNSFW()) {
		t.Fatalf("NSFW body mismatch: %s", hex.EncodeToString(nsfwBody))
	}
	var bd map[string]string
	if err := json.Unmarshal(bdayBody, &bd); err != nil {
		t.Fatalf("birth-date body not JSON: %v", err)
	}
	if _, ok := bd["birthDate"]; !ok {
		t.Fatalf("birth-date body missing birthDate: %s", bdayBody)
	}
	if !strings.Contains(cookie, "sso=SSOTOKEN") || !strings.Contains(cookie, "sso-rw=SSOTOKEN") || !strings.Contains(cookie, "cf_clearance=CFCLEAR") {
		t.Fatalf("cookie header = %q", cookie)
	}
}

// TestRunPartialFailure confirms a non-2xx on one step still lets later steps run.
func TestRunPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bday" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tosURL, bdayURL, nsfwURL = srv.URL+"/tos", srv.URL+"/bday", srv.URL+"/nsfw"

	res, err := Run(context.Background(), "tok", "", Options{Timeout: 5 * time.Second}, nil)
	if err == nil {
		t.Fatalf("expected error from failed birth-date step")
	}
	if !res.TOS || res.BirthDate || !res.NSFW {
		t.Fatalf("expected {TOS:true BirthDate:false NSFW:true}, got %+v", res)
	}
}

func TestRunEmptySSO(t *testing.T) {
	if _, err := Run(context.Background(), "", "", Options{}, nil); err == nil {
		t.Fatal("expected error for empty sso")
	}
}

func checkGRPCHeaders(t *testing.T, r *http.Request, wantCT, wantOrigin string) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != wantCT {
		t.Errorf("%s content-type = %q, want %q", r.URL.Path, ct, wantCT)
	}
	if r.Header.Get("X-Grpc-Web") != "1" {
		t.Errorf("%s missing X-Grpc-Web=1", r.URL.Path)
	}
	if r.Header.Get("X-User-Agent") != "connect-es/2.1.1" {
		t.Errorf("%s X-User-Agent = %q", r.URL.Path, r.Header.Get("X-User-Agent"))
	}
	if origin := r.Header.Get("Origin"); origin != wantOrigin {
		t.Errorf("%s Origin = %q, want %q", r.URL.Path, origin, wantOrigin)
	}
}
