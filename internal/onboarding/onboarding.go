// Package onboarding performs the post-registration account activation steps
// (accept TOS, set birth date, enable NSFW) against the grok.com / accounts.x.ai
// gRPC-web and REST endpoints. It mirrors the reference implementation's byte
// layout so the server accepts the requests identically.
//
// All three steps are best-effort: a failure is recorded in the returned Result
// but does not abort the remaining steps. The caller (the registration pipeline)
// treats the whole operation as non-fatal — an account that registered and went
// through OAuth is still considered successful even if onboarding partly fails.
package onboarding

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/grok-free-register/grok-reg/internal/logx"
)

const (
	accountsHost = "https://accounts.x.ai"
	grokHost     = "https://grok.com"

	nsfwFlag = "always_show_nsfw_content"
)

// Endpoint URLs are vars so tests can redirect them at a httptest.Server.
var (
	tosURL  = accountsHost + "/auth_mgmt.AuthManagement/SetTosAcceptedVersion"
	bdayURL = grokHost + "/rest/auth/set-birth-date"
	nsfwURL = grokHost + "/auth_mgmt.AuthManagement/UpdateUserFeatureControls"
)

// Options carries the network configuration shared by all onboarding requests.
type Options struct {
	Proxy     string // outbound HTTP proxy (REGISTER_PROXY); empty = direct
	UserAgent string // User-Agent header (usually the clearance-reported UA)
	Timeout   time.Duration
}

// Result records which steps succeeded.
type Result struct {
	TOS       bool
	BirthDate bool
	NSFW      bool
}

// Run executes the three activation steps in order against the account bound to
// sso. cfClearance is an optional Cloudflare clearance cookie value. The first
// step error is returned (if any); per-step outcomes are in res. A nil error
// only means no step short-circuited with an error — callers should still check
// res for partial success.
func Run(ctx context.Context, sso, cfClearance string, opt Options, log *logx.Logger) (Result, error) {
	if sso == "" {
		return Result{}, fmt.Errorf("onboarding: empty sso token")
	}
	if opt.UserAgent == "" {
		opt.UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 15 * time.Second
	}
	client := newClient(opt)
	cookie := buildCookie(sso, cfClearance)

	res := Result{}
	var firstErr error

	if ok, err := setTOS(ctx, client, cookie, opt.UserAgent, log); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		logWarnf(log, "onboarding TOS 失败: %v", err)
	} else if ok {
		res.TOS = true
	}

	if ok, err := setBirthDate(ctx, client, cookie, opt.UserAgent, log); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		logWarnf(log, "onboarding 生日设置失败: %v", err)
	} else if ok {
		res.BirthDate = true
	}

	if ok, err := updateNSFW(ctx, client, cookie, opt.UserAgent, log); err != nil {
		if firstErr == nil {
			firstErr = err
		}
		logWarnf(log, "onboarding NSFW 失败: %v", err)
	} else if ok {
		res.NSFW = true
	}

	return res, firstErr
}

func logWarnf(log *logx.Logger, format string, args ...any) {
	if log != nil {
		log.Warnf(format, args...)
	}
}

func logDebugf(log *logx.Logger, format string, args ...any) {
	if log != nil {
		log.Debugf(format, args...)
	}
}

func newClient(opt Options) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // proxies may MITM; matches protocol.Client
	}
	if opt.Proxy != "" {
		if u, err := url.Parse(opt.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{
		Timeout:   opt.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func buildCookie(sso, cfClearance string) string {
	cookie := "sso=" + sso + "; sso-rw=" + sso
	if cfClearance != "" {
		cookie += "; cf_clearance=" + cfClearance
	}
	return cookie
}

// setTOS accepts the terms of service. The protobuf payload encodes field 2 as
// a boolean true: tag 0x10 (field 2, wire type 0) followed by 0x01.
func setTOS(ctx context.Context, client *http.Client, cookie, ua string, log *logx.Logger) (bool, error) {
	inner := []byte{0x10, 0x01} // field 2 = bool true
	body := grpcWebFrame(inner)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tosURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("X-Grpc-Web", "1")
	req.Header.Set("X-User-Agent", "connect-es/2.1.1")
	req.Header.Set("Origin", accountsHost)
	req.Header.Set("Referer", accountsHost+"/accept-tos")
	setCommonHeaders(req, cookie, ua)
	return postStep(client, req, "set_tos", log)
}

// setBirthDate sets a random-of-age birth date via the grok.com REST endpoint.
func setBirthDate(ctx context.Context, client *http.Client, cookie, ua string, log *logx.Logger) (bool, error) {
	payload, _ := json.Marshal(map[string]string{"birthDate": randomBirthDate()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bdayURL, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", grokHost)
	req.Header.Set("Referer", grokHost+"/")
	setCommonHeaders(req, cookie, ua)
	return postStep(client, req, "set_birth_date", log)
}

// updateNSFW enables the always_show_nsfw_content feature control. The payload
// nests a feature-flag message in field 1 and the flag name in field 2.
func updateNSFW(ctx context.Context, client *http.Client, cookie, ua string, log *logx.Logger) (bool, error) {
	body := encodeNSFW()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nsfwURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("X-Grpc-Web", "1")
	req.Header.Set("Origin", grokHost)
	req.Header.Set("Referer", grokHost+"/")
	setCommonHeaders(req, cookie, ua)
	return postStep(client, req, "update_nsfw", log)
}

func setCommonHeaders(req *http.Request, cookie, ua string) {
	req.Header.Set("User-Agent", ua)
	req.Header.Set("X-User-Agent", "connect-es/2.1.1")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", cookie)
}

func postStep(client *http.Client, req *http.Request, step string, log *logx.Logger) (bool, error) {
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s: %w", step, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	if isCloudflareBlock(resp) {
		return false, fmt.Errorf("%s: 被 Cloudflare 拦截 (HTTP %d)", step, resp.StatusCode)
	}
	logDebugf(log, "%s HTTP %d", step, resp.StatusCode)
	return false, fmt.Errorf("%s: HTTP %d", step, resp.StatusCode)
}

// isCloudflareBlock reports a likely Cloudflare interception on the blocking
// status codes when the response is served by Cloudflare or looks like a CF
// challenge page.
func isCloudflareBlock(resp *http.Response) bool {
	switch resp.StatusCode {
	case 403, 429, 503:
	default:
		return false
	}
	server := resp.Header.Get("Server")
	if containsFold(server, "cloudflare") {
		return true
	}
	ct := resp.Header.Get("Content-Type")
	return containsFold(ct, "text/html")
}

func containsFold(s, substr string) bool {
	return bytes.Contains([]byte(bytes.ToLower([]byte(s))), []byte(bytes.ToLower([]byte(substr))))
}

// randomBirthDate returns an ISO-8601 timestamp for someone roughly 20-40 years
// old, matching the reference implementation's onboarding payload.
func randomBirthDate() string {
	now := time.Now().UTC()
	age := 21 + rand.Intn(20) // 21..40 — skewed slightly older so the reported
	// age (which may round down by one within the birth year) stays >= 20.
	year := now.Year() - age
	month := 1 + rand.Intn(12)
	day := 1 + rand.Intn(28)
	return time.Date(year, time.Month(month), day, 16, 0, 0, 0, time.UTC).
		Format("2006-01-02T15:04:05.000Z")
}

// encodeNSFW builds the gRPC-web frame for UpdateUserFeatureControls:
//
//	field 1 (message) = { field 2 (bool true) = 0x10 0x01 }
//	field 2 (message) = { field 1 (string) = "always_show_nsfw_content" }
func encodeNSFW() []byte {
	innerField2 := []byte{0x10, 0x01}                     // bool true
	field1 := pbBytes(1, innerField2)                     // 0x0A <len> 0x10 0x01
	field2 := pbBytes(2, pbStr(1, nsfwFlag))              // 0x12 <len> 0x0A <len> <flag>
	return grpcWebFrame(append(field1, field2...))
}

// pbStr encodes a length-prefixed string in a protobuf field.
func pbStr(field int, s string) []byte {
	return pbBytes(field, []byte(s))
}

// pbBytes encodes a length-delimited (wire type 2) field: tag, varint length, payload.
func pbBytes(field int, payload []byte) []byte {
	out := make([]byte, 0, 2+len(payload))
	out = append(out, byte(field<<3|2))
	out = append(out, pbVarint(len(payload))...)
	out = append(out, payload...)
	return out
}

func pbVarint(n int) []byte {
	var parts []byte
	for n > 0x7f {
		parts = append(parts, byte(n&0x7f)|0x80)
		n >>= 7
	}
	return append(parts, byte(n))
}

// grpcWebFrame prefixes a 5-byte grpc-web header (0x00 + big-endian length).
func grpcWebFrame(inner []byte) []byte {
	frame := make([]byte, 5+len(inner))
	frame[0] = 0
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(inner)))
	copy(frame[5:], inner)
	return frame
}
