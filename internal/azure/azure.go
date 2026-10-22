// Package azure holds the Azure SDK plumbing shared by every client, ARM or
// not: the credential and the client options. It knows nothing about any
// resource type.
package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const (
	// maxApplicationIDLen is azcore's limit on the user agent application ID.
	maxApplicationIDLen = 24

	// tokenRefreshMargin is how long before expiry a token without a RefreshOn
	// is refreshed. It matches azcore's bearer token policy, so a cached token
	// is never one the policy would refresh straight away.
	tokenRefreshMargin = 5 * time.Minute
)

// ErrSignIn reports that no credential could get a token. Requests fetch
// tokens lazily, so it surfaces from the first API call, wrapped in that call's
// error. Callers can match it with errors.Is.
var ErrSignIn = errors.New("signing in to Azure")

// NewCredential returns the SDK's default credential chain: a service
// principal from the environment, workload identity, managed identity, then
// the Azure CLI, Azure Developer CLI and Azure PowerShell sign-ins. It caches
// tokens for the run, shared by every client, and its errors wrap ErrSignIn.
func NewCredential() (azcore.TokenCredential, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	return &credential{cred: cred, tokens: map[tokenKey]azcore.AccessToken{}}, nil
}

// credential is the run's one credential. It hands every client the same
// token per scope and tenant, so the sign-in chain (often a subprocess such as
// 'az') runs once per run rather than once per client. Each client's bearer
// token policy caches only its own token, and the CLI credentials do not cache
// at all.
//
// Its errors wrap ErrSignIn, because azidentity's own "credential unavailable"
// error type is unexported and cannot be matched directly.
type credential struct {
	cred   azcore.TokenCredential
	mu     sync.Mutex // held while fetching, so concurrent callers wait for one fetch
	tokens map[tokenKey]azcore.AccessToken
	err    error // a sign-in failure, returned to later callers instead of retrying
}

// tokenKey identifies the tokens that can stand in for one another.
type tokenKey struct {
	scopes    string
	tenantID  string
	enableCAE bool
}

func (c *credential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	key := tokenKey{scopes: strings.Join(opts.Scopes, " "), tenantID: opts.TenantID, enableCAE: opts.EnableCAE}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Claims come from a service's claims challenge, such as Continuous Access
	// Evaluation revoking the token: the cached token is the one that was
	// rejected, so fetch a new one and replace it.
	if opts.Claims == "" {
		if c.err != nil {
			return azcore.AccessToken{}, c.err
		}

		if tok, ok := c.tokens[key]; ok && !dueForRefresh(tok) {
			return tok, nil
		}
	}

	tok, err := c.cred.GetToken(ctx, opts)

	switch {
	case err == nil:
		c.tokens[key] = tok

		return tok, nil
	case ctx.Err() != nil:
		// A cancelled or timed-out run is not a sign-in failure.
		return tok, err
	default:
		err = fmt.Errorf("%w: %w", ErrSignIn, err)
		if opts.Claims == "" {
			c.err = err // signing in will not start working during the run
		}

		return tok, err
	}
}

// dueForRefresh applies azcore's bearer token policy rule: refresh at
// RefreshOn when the authority suggested one, otherwise shortly before expiry.
func dueForRefresh(tok azcore.AccessToken) bool {
	if !tok.RefreshOn.IsZero() {
		return !time.Now().Before(tok.RefreshOn)
	}

	return !time.Now().Before(tok.ExpiresOn.Add(-tokenRefreshMargin))
}

// ClientOptions returns the options shared by every Azure client: a user agent
// identifying azskufinder and the SDK's default retries. ARM adapters wrap it
// in arm.ClientOptions; non-ARM clients pass it to runtime.NewPipeline.
func ClientOptions(version string) policy.ClientOptions {
	return policy.ClientOptions{
		Telemetry: policy.TelemetryOptions{
			ApplicationID: applicationID(version),
		},
		// Retry is left at the SDK defaults: three retries with exponential
		// backoff, honouring Retry-After on throttling. The --timeout context
		// bounds the whole run.
		Logging: policy.LogOptions{
			// azcore redacts query values unless allowed, which would hide
			// the $filter in debug output.
			AllowedQueryParams: []string{"$filter", "api-version"},
		},
	}
}

// applicationID returns the user agent application ID, clamped to azcore's
// length limit so a long build version cannot break it.
func applicationID(version string) string {
	id := "azskufinder/" + version
	if len(id) > maxApplicationIDLen {
		id = id[:maxApplicationIDLen]
	}

	return id
}

// ErrorText returns err's message for a person to read. The SDK writes an
// Azure response error as a block of lines holding the request, the status and
// the whole response body; that block is replaced by one line: the service's
// own message, then its error code and HTTP status. Other errors are returned
// as they are. Debug logging still records the full response.
func ErrorText(err error) string {
	text := err.Error()

	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) {
		return text
	}

	return strings.Replace(text, respErr.Error(), responseText(respErr), 1)
}

// responseText is the one-line form of an Azure response error, such as
// "The provided subscription identifier '…' is malformed or invalid.
// (InvalidSubscriptionId, HTTP 400)".
func responseText(e *azcore.ResponseError) string {
	var body struct {
		// ARM writes "error" and the Retail Prices API "Error"; encoding/json
		// matches either.
		Error struct {
			Code    string
			Message string
		}
	}

	if e.RawResponse != nil {
		// Payload reads the body that NewResponseError buffered.
		if raw, err := runtime.Payload(e.RawResponse); err == nil {
			_ = json.Unmarshal(raw, &body)
		}
	}

	code := e.ErrorCode
	if code == "" {
		code = body.Error.Code
	}

	msg := firstSentence(strings.TrimSpace(body.Error.Message))
	if msg == "" {
		msg = fmt.Sprintf("Azure answered %d %s", e.StatusCode, http.StatusText(e.StatusCode))
		if code == "" {
			return msg
		}

		return msg + " (" + code + ")"
	}

	if code == "" {
		return fmt.Sprintf("%s (HTTP %d)", msg, e.StatusCode)
	}

	return fmt.Sprintf("%s (%s, HTTP %d)", msg, code, e.StatusCode)
}

// maxMessageLen is the longest service message shown whole. Some are much
// longer: an unknown location lists every API version and location.
const maxMessageLen = 200

// firstSentence returns a long message cut after its first sentence.
func firstSentence(msg string) string {
	if len(msg) <= maxMessageLen {
		return msg
	}

	if i := strings.Index(msg, ". "); i >= 0 {
		return msg[:i+1]
	}

	return msg
}

// EnableSDKLogging routes azcore and azidentity logging to logger at debug
// level, and does nothing unless logger has debug enabled. Call it before
// NewCredential, which logs which credentials it could set up.
//
// It installs a process-wide azcore log listener as a side effect: azcore
// offers no per-client logger, and a CLI only needs one.
func EnableSDKLogging(logger *slog.Logger) {
	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}

	log.SetEvents(
		log.EventRequest,
		log.EventResponse,
		log.EventResponseError,
		log.EventRetryPolicy,
		azidentity.EventAuthentication,
	)
	log.SetListener(listener(logger.With("component", "azure-sdk")))
}

// listener adapts logger to an azcore log listener.
func listener(logger *slog.Logger) func(log.Event, string) {
	return func(ev log.Event, msg string) {
		logger.Debug(msg, "event", string(ev))
	}
}
