package azure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// fakeCredential is the credential the wrapper calls. It counts fetches and
// returns whatever getToken returns.
type fakeCredential struct {
	calls    atomic.Int32
	getToken func(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error)
}

func (f *fakeCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	f.calls.Add(1)

	return f.getToken(ctx, opts)
}

func newTestCredential(f *fakeCredential) *credential {
	return &credential{cred: f, tokens: map[tokenKey]azcore.AccessToken{}}
}

// fetch is one answer from fakeCredential: a token named token, or err.
type fetch struct {
	token string
	err   error
}

// queuedCredential returns a fake that answers each fetch with the next of
// fetches, or the context's error once it is done. The wrapper's mutex
// serialises fetches, so the queue needs no lock of its own.
func queuedCredential(t *testing.T, fetches ...fetch) *fakeCredential {
	t.Helper()

	var next int

	return &fakeCredential{getToken: func(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
		if err := ctx.Err(); err != nil {
			return azcore.AccessToken{}, err
		}

		if next == len(fetches) {
			t.Errorf("unexpected fetch %d", next+1)

			return azcore.AccessToken{}, errors.New("unexpected fetch")
		}

		f := fetches[next]
		next++

		if f.err != nil {
			return azcore.AccessToken{}, f.err
		}

		return azcore.AccessToken{Token: f.token, ExpiresOn: time.Now().Add(time.Hour)}, nil
	}}
}

func TestCredential_GetToken(t *testing.T) {
	t.Parallel()

	errNoCredential := errors.New("no credential available")

	plain := policy.TokenRequestOptions{Scopes: []string{"https://management.azure.com/.default"}}

	with := func(change func(*policy.TokenRequestOptions)) policy.TokenRequestOptions {
		opts := plain
		change(&opts)

		return opts
	}
	claims := with(func(o *policy.TokenRequestOptions) { o.Claims = `{"access_token":{}}` })

	// call is one GetToken call and what it should return: wantToken, or an
	// error matching wantErr, which wraps ErrSignIn when wantSignIn is set.
	type call struct {
		opts       policy.TokenRequestOptions
		cancelled  bool
		wantToken  string
		wantErr    error
		wantSignIn bool
	}

	tests := []struct {
		name      string
		fetches   []fetch
		calls     []call
		wantCalls int32
	}{
		{
			name:    "caches the token",
			fetches: []fetch{{token: "t1"}},
			calls: []call{
				{opts: plain, wantToken: "t1"},
				{opts: plain, wantToken: "t1"},
			},
			wantCalls: 1,
		},
		{
			name:    "caches per scope, tenant and CAE",
			fetches: []fetch{{token: "t1"}, {token: "t2"}, {token: "t3"}, {token: "t4"}},
			calls: []call{
				{opts: plain, wantToken: "t1"},
				{opts: with(func(o *policy.TokenRequestOptions) { o.Scopes = []string{"https://other/.default"} }), wantToken: "t2"},
				{opts: with(func(o *policy.TokenRequestOptions) { o.TenantID = "tenant" }), wantToken: "t3"},
				{opts: with(func(o *policy.TokenRequestOptions) { o.EnableCAE = true }), wantToken: "t4"},
				{opts: plain, wantToken: "t1"},
			},
			wantCalls: 4,
		},
		{
			name:    "claims replace the cached token",
			fetches: []fetch{{token: "t1"}, {token: "t2"}},
			calls: []call{
				{opts: plain, wantToken: "t1"},
				{opts: claims, wantToken: "t2"},
				{opts: plain, wantToken: "t2"},
			},
			wantCalls: 2,
		},
		{
			name:    "sign-in error is kept for later calls",
			fetches: []fetch{{err: errNoCredential}},
			calls: []call{
				{opts: plain, wantErr: errNoCredential, wantSignIn: true},
				{opts: plain, wantErr: errNoCredential, wantSignIn: true},
			},
			wantCalls: 1,
		},
		{
			name:    "sign-in error from claims is not kept",
			fetches: []fetch{{err: errNoCredential}, {token: "t1"}},
			calls: []call{
				{opts: claims, wantErr: errNoCredential, wantSignIn: true},
				{opts: plain, wantToken: "t1"},
			},
			wantCalls: 2,
		},
		{
			name:    "claims fetch after a kept sign-in error",
			fetches: []fetch{{err: errNoCredential}, {token: "t1"}},
			calls: []call{
				{opts: plain, wantErr: errNoCredential, wantSignIn: true},
				{opts: claims, wantToken: "t1"},
			},
			wantCalls: 2,
		},
		{
			name:    "cancelled context is not a sign-in error",
			fetches: []fetch{{token: "t1"}},
			calls: []call{
				{opts: plain, cancelled: true, wantErr: context.Canceled},
				{opts: plain, wantToken: "t1"},
			},
			wantCalls: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := queuedCredential(t, tc.fetches...)
			cred := newTestCredential(fake)

			for i, c := range tc.calls {
				ctx := t.Context()
				if c.cancelled {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}

				tok, err := cred.GetToken(ctx, c.opts)

				switch {
				case c.wantErr != nil:
					if !errors.Is(err, c.wantErr) {
						t.Errorf("call %d: GetToken() error = %v, want %v", i+1, err, c.wantErr)
					}

					if got := errors.Is(err, ErrSignIn); got != c.wantSignIn {
						t.Errorf("call %d: errors.Is(%v, ErrSignIn) = %t, want %t", i+1, err, got, c.wantSignIn)
					}
				case err != nil:
					t.Errorf("call %d: GetToken() error = %v, want token %q", i+1, err, c.wantToken)
				case tok.Token != c.wantToken:
					t.Errorf("call %d: GetToken() = %q, want %q", i+1, tok.Token, c.wantToken)
				}
			}

			if got := fake.calls.Load(); got != tc.wantCalls {
				t.Errorf("fetches = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestCredential_GetTokenRefresh(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var n int

		fake := &fakeCredential{getToken: func(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
			n++

			return azcore.AccessToken{Token: "t" + string(rune('0'+n)), ExpiresOn: time.Now().Add(10 * time.Minute)}, nil
		}}
		cred := newTestCredential(fake)
		opts := policy.TokenRequestOptions{Scopes: []string{"scope"}}

		if _, err := cred.GetToken(t.Context(), opts); err != nil {
			t.Fatalf("GetToken() error = %v", err)
		}

		// 4 minutes before expiry is inside the refresh margin.
		time.Sleep(6 * time.Minute)

		tok, err := cred.GetToken(t.Context(), opts)
		if err != nil {
			t.Fatalf("GetToken() error = %v", err)
		}

		if tok.Token != "t2" {
			t.Errorf("GetToken() after 6m = %q, want %q", tok.Token, "t2")
		}

		if tok, _ := cred.GetToken(t.Context(), opts); tok.Token != "t2" {
			t.Errorf("GetToken() after refresh = %q, want %q", tok.Token, "t2")
		}

		if got := fake.calls.Load(); got != 2 {
			t.Errorf("fetches = %d, want 2", got)
		}
	})
}

// TestCredential_GetTokenConcurrent does not use synctest: goroutines waiting
// on a sync.Mutex are not durably blocked, so the bubble's clock would never
// move. It needs no timing either, because a caller that arrives after the
// fetch finds the token cached, so one fetch is right for every interleaving.
func TestCredential_GetTokenConcurrent(t *testing.T) {
	t.Parallel()

	fake := &fakeCredential{getToken: func(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
		return azcore.AccessToken{Token: "t1", ExpiresOn: time.Now().Add(time.Hour)}, nil
	}}
	cred := newTestCredential(fake)
	opts := policy.TokenRequestOptions{Scopes: []string{"scope"}}

	const callers = 16

	var (
		start = make(chan struct{})
		wg    sync.WaitGroup
		got   [callers]string
		errs  [callers]error
	)

	for i := range callers {
		wg.Go(func() {
			<-start

			tok, err := cred.GetToken(t.Context(), opts)
			got[i], errs[i] = tok.Token, err
		})
	}

	close(start)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil || got[i] != "t1" {
			t.Errorf("caller %d: GetToken() = %q, %v, want %q, nil", i, got[i], errs[i], "t1")
		}
	}

	if n := fake.calls.Load(); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}
}

func TestDueForRefresh(t *testing.T) {
	t.Parallel()

	// Offsets from now. A zero refreshOn means the token has no RefreshOn.
	tests := []struct {
		name      string
		expiresIn time.Duration
		refreshIn time.Duration
		noRefresh bool
		want      bool
	}{
		{name: "expiry outside margin", expiresIn: 10 * time.Minute, noRefresh: true, want: false},
		{name: "expiry at margin", expiresIn: tokenRefreshMargin, noRefresh: true, want: true},
		{name: "expiry inside margin", expiresIn: 4 * time.Minute, noRefresh: true, want: true},
		{name: "expired", expiresIn: -time.Minute, noRefresh: true, want: true},
		{name: "refresh on in future overrides close expiry", expiresIn: 2 * time.Minute, refreshIn: time.Minute, want: false},
		{name: "refresh on now", expiresIn: time.Hour, refreshIn: 0, want: true},
		{name: "refresh on passed", expiresIn: time.Hour, refreshIn: -time.Minute, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				now := time.Now()

				tok := azcore.AccessToken{ExpiresOn: now.Add(tc.expiresIn)}
				if !tc.noRefresh {
					tok.RefreshOn = now.Add(tc.refreshIn)
				}

				if got := dueForRefresh(tok); got != tc.want {
					t.Errorf("dueForRefresh(expires in %v, refresh in %v) = %t, want %t", tc.expiresIn, tc.refreshIn, got, tc.want)
				}
			})
		})
	}
}

func TestApplicationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "empty", version: "", want: "azskufinder/"},
		{name: "short", version: "1.2.3", want: "azskufinder/1.2.3"},
		{name: "at limit", version: "1.2.3-beta.1", want: "azskufinder/1.2.3-beta.1"},
		{name: "over limit", version: "1.2.3-beta.1+abcdef", want: "azskufinder/1.2.3-beta.1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := applicationID(tc.version); got != tc.want {
				t.Errorf("applicationID(%q) = %q, want %q", tc.version, got, tc.want)
			}
		})
	}
}

func TestErrorText(t *testing.T) {
	t.Parallel()

	// respond builds the error azcore returns for an HTTP response.
	respond := func(t *testing.T, status int, body string) error {
		t.Helper()

		return runtime.NewResponseError(&http.Response{
			Status:     http.StatusText(status),
			StatusCode: status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://management.azure.com/subscriptions", nil),
		})
	}

	long := "No registered resource provider found for location 'nowhere'. The supported locations are " +
		strings.Repeat("uksouth, ", 30) + "westus."

	tests := []struct {
		name string
		err  func(t *testing.T) error
		want string
	}{
		{
			name: "ARM error",
			err: func(t *testing.T) error {
				return fmt.Errorf("listing SKUs: %w", respond(t, 400,
					`{"error":{"code":"InvalidSubscriptionId","message":"The subscription is invalid."}}`))
			},
			want: "listing SKUs: The subscription is invalid. (InvalidSubscriptionId, HTTP 400)",
		},
		{
			name: "Retail Prices error, with a capital E and no code header",
			err: func(t *testing.T) error {
				return fmt.Errorf("querying retail prices: %w", respond(t, 500,
					`{"Error":{"Code":"InternalServerError","Message":"Something broke"}}`))
			},
			want: "querying retail prices: Something broke (InternalServerError, HTTP 500)",
		},
		{
			name: "long message cut after its first sentence",
			err: func(t *testing.T) error {
				return respond(t, 400, `{"error":{"code":"NoRegisteredProviderFound","message":"`+long+`"}}`)
			},
			want: "No registered resource provider found for location 'nowhere'. (NoRegisteredProviderFound, HTTP 400)",
		},
		{
			name: "empty body",
			err:  func(t *testing.T) error { return respond(t, 503, "") },
			want: "Azure answered 503 Service Unavailable",
		},
		{
			name: "body that is not JSON",
			err:  func(t *testing.T) error { return respond(t, 502, "<html>bad gateway</html>") },
			want: "Azure answered 502 Bad Gateway",
		},
		{
			name: "text around the response error is kept",
			err: func(t *testing.T) error {
				return fmt.Errorf("throttled (%w), try later", respond(t, 429, `{"error":{"code":"TooMany","message":"Slow down."}}`))
			},
			want: "throttled (Slow down. (TooMany, HTTP 429)), try later",
		},
		{
			name: "not an Azure error",
			err:  func(*testing.T) error { return errors.New("boom") },
			want: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.err(t)
			if got := ErrorText(err); got != tc.want {
				t.Errorf("ErrorText(%q) = %q, want %q", err, got, tc.want)
			}
		})
	}
}

// fakeTransport answers every request with an empty 200.
type fakeTransport struct{}

func (fakeTransport) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
}

// TestEnableSDKLogging is the package's only sequential test, because it sets
// azcore's process-wide log listener. Top-level tests that call t.Parallel
// resume only after every sequential top-level test has returned, so none of
// them run while the listener is set.
func TestEnableSDKLogging(t *testing.T) { //nolint:paralleltest // sets azcore's process-wide log listener
	t.Cleanup(func() {
		log.SetListener(nil)
		log.SetEvents()
	})

	newLogger := func(level slog.Level) (*slog.Logger, *bytes.Buffer) {
		var buf bytes.Buffer

		return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})), &buf
	}

	debugLogger, debugBuf := newLogger(slog.LevelDebug)
	infoLogger, infoBuf := newLogger(slog.LevelInfo)

	EnableSDKLogging(debugLogger)
	// Without debug enabled it does nothing, so the debug listener stays.
	EnableSDKLogging(infoLogger)

	opts := ClientOptions("test")
	opts.Transport = fakeTransport{}
	pl := runtime.NewPipeline("test", "v1", runtime.PipelineOptions{}, &opts)

	// ClientOptions allows $filter, so the debug log shows the query asked for,
	// while any other query value is still redacted.
	req, err := runtime.NewRequest(t.Context(), http.MethodGet, "https://example.invalid/?$filter=location&other=secret")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := pl.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = resp.Body.Close()

	got := debugBuf.String()
	for _, want := range []string{"level=DEBUG", "component=azure-sdk", "event=Request", "filter=location", "other=REDACTED"} {
		if !strings.Contains(got, want) {
			t.Errorf("debug log missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "secret") {
		t.Errorf("debug log contains a query value that should be redacted:\n%s", got)
	}

	if infoBuf.Len() != 0 {
		t.Errorf("EnableSDKLogging(info logger) installed a listener, logged:\n%s", infoBuf)
	}
}
