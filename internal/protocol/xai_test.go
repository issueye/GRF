package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildSignupBodyIncludesCastle verifies the Castle token is wired into the
// signup JSON body (field castleRequestToken) instead of the old empty string.
func TestBuildSignupBodyIncludesCastle(t *testing.T) {
	body := BuildSignupBody("user@example.com", "pw", "ABC-123", "tt-token", "castle-xyz")
	var payload []any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v\n%s", err, body)
	}
	if len(payload) < 1 {
		t.Fatalf("payload empty")
	}
	first, ok := payload[0].(map[string]any)
	if !ok {
		t.Fatalf("payload[0] not object: %v", payload[0])
	}
	if got := first["castleRequestToken"]; got != "castle-xyz" {
		t.Fatalf("castleRequestToken = %v, want castle-xyz", got)
	}
	if got := first["turnstileToken"]; got != "tt-token" {
		t.Fatalf("turnstileToken = %v, want tt-token", got)
	}
	// conversionId must be a non-empty string (random UUID).
	if cid, _ := first["conversionId"].(string); cid == "" {
		t.Fatalf("conversionId missing/empty: %v", first["conversionId"])
	}
}

// TestCreateEmailCodeEmitsCastleField3 verifies the protobuf body carries the
// Castle token in field 3 (wire type 2) when provided, and omits it when empty.
func TestCreateEmailCodeEmitsCastleField3(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("grpc-status", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	original := ConnectCreate
	ConnectCreate = srv.URL
	defer func() { ConnectCreate = original }()

	c, err := NewClient("", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// With castle token: inner = field1(email) + field3(castle).
	if err := c.CreateEmailCode("user@example.com", "castle-tok"); err != nil {
		t.Fatalf("CreateEmailCode with castle: %v", err)
	}
	frame1 := append([]byte(nil), gotBody...)
	if len(frame1) < 5 {
		t.Fatalf("body too short")
	}
	// grpc-web prefix is 0x00 + 4-byte length; the rest is the protobuf.
	inner1 := frame1[5:]
	if !bytes.Contains(inner1, []byte("castle-tok")) {
		t.Fatalf("castle token not in protobuf inner: %x", inner1)
	}
	// Expect at least two length-delimited fields (email + castle).
	if bytes.Count(inner1, []byte("user@example.com")) != 1 {
		t.Fatalf("email not present once: %x", inner1)
	}

	// Without castle token: inner must contain only the email field.
	if err := c.CreateEmailCode("user@example.com", ""); err != nil {
		t.Fatalf("CreateEmailCode without castle: %v", err)
	}
	inner2 := gotBody[5:]
	if bytes.Contains(inner2, []byte("castle-tok")) {
		t.Fatalf("unexpected castle token when empty: %x", inner2)
	}
	// The inner should be short (just the email field) — no field-3 tag present.
	// field 3 tag (wire 2) = 0x1A; ensure it is not present when castle is empty.
	if bytes.ContainsRune(inner2, 0x1A) {
		// 0x1A could appear inside email bytes; guard by checking the email has no such byte.
		if !strings.Contains("user@example.com", string(rune(0x1A))) {
			t.Fatalf("field-3 tag present without castle: %x", inner2)
		}
	}
}

// TestCreateEmailCodeErrorOnBadStatus ensures a non-0 grpc-status surfaces an error.
func TestCreateEmailCodeErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("grpc-status", "7")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	original := ConnectCreate
	ConnectCreate = srv.URL
	defer func() { ConnectCreate = original }()

	c, err := NewClient("", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.CreateEmailCode("user@example.com", ""); err == nil {
		t.Fatal("expected error on grpc-status=7")
	}
	_ = context.Background()
}
