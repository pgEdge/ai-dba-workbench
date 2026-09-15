/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/oidc"
)

// OIDCStartPath and OIDCCallbackPath are the two endpoints a federated
// login touches: the browser is sent to the first by the login page, and
// the identity provider sends it back to the second. Both are in the
// unauthenticated path list in internal/auth/middleware.go, for the
// obvious reason that a user starting a login does not yet have a
// session.
//
// The values come from internal/config, which is where configuration
// validation needs them in order to check redirect_url against the
// callback path; these names exist so that the handlers below read as
// handlers rather than as configuration.
const (
	OIDCStartPath    = config.OIDCStartPath
	OIDCCallbackPath = config.OIDCCallbackPath
)

// loginErrorProvider and loginErrorFailed are the only two values that
// ever appear in the login_error query parameter the browser is sent
// back to.
//
// There are deliberately two and not more. loginErrorProvider says the
// identity provider itself declined (the user canceled at the consent
// screen, most often), which is worth distinguishing because the login
// page can then say "the provider refused" rather than implying the
// Workbench did. loginErrorFailed covers everything on this side of the
// redirect without distinguishing any of it: a rejected email domain, an
// unknown subject, a disabled account, a username collision, an account
// that is not federated, a failed group reconciliation and a failed
// session all produce exactly this, byte for byte, because the
// difference between them is precisely what an attacker holding a
// self-registered identity at a permissive provider would use to
// enumerate local accounts. The real reason goes to the server log.
const (
	loginErrorProvider = "provider"
	loginErrorFailed   = "login"
)

// loginFailedTarget and providerFailedTarget are the fixed redirect
// targets for the two cases above. They are constants rather than
// anything built per request, so that no provider-supplied text can
// reach a Location header.
const (
	loginFailedTarget    = "/?" + loginErrorParam + "=" + loginErrorFailed
	providerFailedTarget = "/?" + loginErrorParam + "=" + loginErrorProvider
)

// loginErrorParam is the query parameter carrying the outcome marker.
const loginErrorParam = "login_error"

// genericCallbackError is the body of every 400 this handler returns. It
// says nothing about which check failed, since the checks it covers
// (absent state cookie, unsealable state cookie, state mismatch, absent
// code) tell an attacker probing the callback how far their forgery got.
const genericCallbackError = "The login request could not be completed"

// exchangeTimeout bounds the authorization code exchange, which reaches
// out to the identity provider's token endpoint and possibly its JWKS
// endpoint. Both go through http.DefaultClient inside the underlying
// libraries, and that client has no timeout of its own, so without this
// a provider that accepts the connection and then stalls would pin this
// callback's goroutine (and the browser behind it) open indefinitely.
const exchangeTimeout = 15 * time.Second

// callbackRateWindowMinutes and callbackRateMaxAttempts bound how often
// one rate-limit key may call the callback. The endpoint drives an
// outbound request to the identity provider on every call that gets as
// far as the exchange, so leaving it unbounded would make it a
// convenient way to have the Workbench hammer someone else's provider.
//
// The ceiling is deliberately far above the twenty per minute
// handleLogin allows, because the key is usually not a client at all.
// Without http.trusted_proxies configured, every request behind a
// reverse proxy shares that proxy's address, so the allowance is one
// per deployment and a low ceiling would let any unauthenticated party
// spend it and take federated login down for everybody. On a deployment
// where local login is switched off, that is the whole way in. A
// completed login returns its allowance (see completeLogin), so the
// figure bounds failures rather than logins, and initOIDC warns at
// start-up when no trusted proxy list makes the key per client.
const (
	callbackRateWindowMinutes = 1
	callbackRateMaxAttempts   = 240
)

// OIDCHandler serves the two endpoints of a federated login.
//
// It owns no session logic of its own: the state cookie comes from
// internal/oidc, the protocol from oidc.Provider, and the user
// resolution, group reconciliation and session creation from
// auth.AuthStore. What lives here is the composition of those, and the
// rule that the browser learns nothing from a failure beyond the fact of
// it.
type OIDCHandler struct {
	authStore federationStore
	provider  *oidc.Provider
	cfg       config.OIDCConfig
	stateKey  []byte

	// rateLimiter is owned by this handler and stopped by Close; it is
	// not the shared failed-login limiter, because a federated callback
	// failing is not a password guess.
	rateLimiter *auth.RateLimiter

	ipExtractor *auth.IPExtractor
	tlsEnabled  bool
}

// federationStore is the slice of auth.AuthStore a federated login
// actually uses: resolve the account, reconcile its groups, mint the
// session. It is an interface rather than the concrete store so that a
// test can drive the failure of any one of the three, including the
// session failure, which on the real store needs the account to change
// between two calls inside one request and is therefore not reachable
// otherwise.
//
// The exported constructor still takes *auth.AuthStore: nothing outside
// this package has any business supplying a different implementation,
// and narrowing the public signature would invite one.
type federationStore interface {
	ResolveFederatedUser(identity auth.FederatedIdentity,
		opts auth.FederationOptions) (*auth.StoredUser, error)
	ReconcileFederatedGroups(userID int64, identity auth.FederatedIdentity,
		opts auth.FederationOptions) error
	CreateSessionForUser(username string) (string, time.Time, error)
}

// NewOIDCHandler creates the federated login handler.
//
// provider may be nil, and cfg.Enabled may be false; either makes both
// endpoints answer 404, so that a Workbench with OIDC switched off is
// indistinguishable from one built without it. stateKey must be the
// 32-byte key the state cookie is sealed with; a wrong-length key is not
// rejected here because oidc.SealState rejects it on the first login,
// which fails closed.
//
// The ipExtractor parameter is optional, matching NewAuthHandler: when
// it is nil, RemoteAddr is used directly and no forwarded header is
// trusted. Whether X-Forwarded-Proto is honored is decided per request
// against the extractor's trusted proxy list, not by the extractor
// merely existing.
func NewOIDCHandler(authStore *auth.AuthStore, provider *oidc.Provider,
	cfg config.OIDCConfig, stateKey []byte, tlsEnabled bool,
	ipExtractor *auth.IPExtractor) *OIDCHandler {

	handler := newOIDCHandler(nil, provider, cfg, stateKey, tlsEnabled, ipExtractor)
	// Assigned only when non-nil, so that a nil *auth.AuthStore does not
	// become a non-nil interface holding a nil pointer, which enabled()
	// would then read as a usable store.
	if authStore != nil {
		handler.authStore = authStore
	}
	return handler
}

// newOIDCHandler is the constructor the tests use to supply a stand-in
// store. Production code goes through NewOIDCHandler.
func newOIDCHandler(authStore federationStore, provider *oidc.Provider,
	cfg config.OIDCConfig, stateKey []byte, tlsEnabled bool,
	ipExtractor *auth.IPExtractor) *OIDCHandler {

	return &OIDCHandler{
		authStore:   authStore,
		provider:    provider,
		cfg:         cfg,
		stateKey:    stateKey,
		rateLimiter: auth.NewRateLimiter(callbackRateWindowMinutes, callbackRateMaxAttempts),
		ipExtractor: ipExtractor,
		tlsEnabled:  tlsEnabled,
	}
}

// RegisterRoutes registers the two federated login endpoints. They are
// deliberately registered without the authentication wrapper: a user
// starting a login has no session yet, and the callback is how they get
// one.
func (h *OIDCHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(OIDCStartPath, h.handleStart)
	mux.HandleFunc(OIDCCallbackPath, h.handleCallback)
}

// Close stops the background cleanup goroutine belonging to the rate
// limiter this handler created, mirroring AuthHandler.Close. Callers
// must invoke it when the handler is torn down, notably in tests, or the
// goroutine outlives it.
func (h *OIDCHandler) Close() {
	if h.rateLimiter != nil {
		h.rateLimiter.Stop()
	}
}

// enabled reports whether federated login is actually available. Both
// conditions matter: an operator can switch OIDC off in the
// configuration, and a handler can be constructed without a provider.
func (h *OIDCHandler) enabled() bool {
	return h.cfg.Enabled && h.provider != nil && h.authStore != nil
}

// handleStart handles GET /api/v1/auth/oidc/start, the endpoint the
// login page's federated login button points at. It mints a fresh login
// state, seals it into a short-lived cookie and redirects the browser to
// the identity provider.
func (h *OIDCHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	// A 404 rather than a 503 or a 400, and before the method check so
	// that every method answers it: a Workbench with federated login
	// switched off should look exactly like one that never had the
	// endpoint, so that probing it says nothing about the deployment.
	if !h.enabled() {
		http.NotFound(w, r)
		return
	}
	if !h.methodIsGET(w, r) {
		return
	}

	// The raw "return" parameter is attacker-controlled; NewLoginState
	// puts it through oidc.SanitiseReturnPath, which is the open-redirect
	// guard, before it is ever stored.
	state, err := oidc.NewLoginState(r.URL.Query().Get("return"))
	if err != nil {
		// Sanitized like every other log call in this file. This one
		// carries no attacker-controlled text today, but uniformity is
		// what stops the next edit being the exception.
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Failed to create login state: %s",
			logging.SanitizeForLog(err.Error()))
		RespondError(w, http.StatusInternalServerError, genericCallbackError)
		return
	}

	sealed, err := oidc.SealState(h.stateKey, state)
	if err != nil {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Failed to seal login state: %s",
			logging.SanitizeForLog(err.Error()))
		RespondError(w, http.StatusInternalServerError, genericCallbackError)
		return
	}

	secure := h.isSecureRequest(r)
	// #nosec G124 -- Secure is conditional on isSecureRequest so that
	// local HTTP development still works; in production behind TLS
	// (direct or via a trusted proxy supplying X-Forwarded-Proto) it
	// evaluates to true, and the cookie name then gains the "__Host-"
	// prefix, which a browser refuses to set without Secure. HttpOnly
	// and SameSite are unconditional.
	http.SetCookie(w, &http.Cookie{
		Name:     oidc.StateCookieNameFor(secure),
		Value:    sealed,
		Path:     "/",
		MaxAge:   int(oidc.StateTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		// SameSite=Lax, not Strict: the callback arrives as a top-level
		// navigation from the identity provider's origin, and Strict
		// would suppress the cookie on exactly that request, making
		// every login fail with a missing state cookie.
		SameSite: http.SameSiteLaxMode,
		// No Domain attribute, deliberately: a host-only cookie cannot
		// be read or overwritten by a sibling subdomain, and the
		// "__Host-" prefix requires its absence.
	})

	redirectTo(w, h.provider.AuthCodeURL(state))
}

// handleCallback handles GET /api/v1/auth/oidc/callback, the endpoint
// the identity provider returns the browser to.
//
// Every failure below answers with a fixed response and logs the real
// reason. Nothing derived from the provider's response, the claims or
// the user store reaches the browser, in the body or in a header: the
// error text from internal/oidc quotes claim values and raw HTTP
// response bodies, and the error text from auth.ResolveFederatedUser
// names local accounts and their exact stored spelling.
func (h *OIDCHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// The 404 comes first, before the method check, so that a disabled
	// endpoint answers exactly as an absent one does for every method.
	if !h.enabled() {
		http.NotFound(w, r)
		return
	}
	if !h.methodIsGET(w, r) {
		return
	}

	// Clear the state cookie before anything else can fail, the rate
	// limit included, so that it is gone on every path out of this
	// function: a state that has been presented once must never be
	// usable again, whether it was accepted, refused or never looked at.
	secure := h.isSecureRequest(r)
	h.clearStateCookie(w, secure)

	ipAddress := h.extractIPFromRequest(r)
	if !h.allowRequest(w, ipAddress) {
		return
	}

	state, ok := h.openPresentedState(w, r, secure)
	if !ok {
		return
	}

	identity, ok := h.exchange(w, r, state)
	if !ok {
		return
	}

	h.completeLogin(w, r, state, identity, ipAddress)
}

// openPresentedState performs every check that can be made before the
// authorization code is touched: that the provider did not report a
// failure, that a state cookie was presented, that it opens with this
// server's key and has not expired, and that the state query parameter
// matches the one sealed inside it.
func (h *OIDCHandler) openPresentedState(w http.ResponseWriter, r *http.Request,
	secure bool) (*oidc.LoginState, bool) {

	query := r.URL.Query()

	// The provider reporting a failure is handled before the state is
	// opened, because there is no code to exchange in that case and the
	// user most likely just canceled at the consent screen.
	if providerError := query.Get("error"); providerError != "" {
		//nolint:gosec // G706: provider-supplied text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Identity provider reported an error: %s (description: %s)",
			logging.SanitizeForLog(providerError),
			logging.SanitizeForLog(query.Get("error_description")))
		redirectTo(w, providerFailedTarget)
		return nil, false
	}

	cookie, err := r.Cookie(oidc.StateCookieNameFor(secure))
	if err != nil || cookie.Value == "" {
		log.Printf("[OIDC] Callback carried no login state cookie")
		RespondError(w, http.StatusBadRequest, genericCallbackError)
		return nil, false
	}

	state, err := oidc.OpenState(h.stateKey, cookie.Value)
	if err != nil {
		// cookie.Value is wholly attacker-controlled, and whether any of
		// it reaches this error is a decision made in another package;
		// sanitize rather than depend on that package's error strings
		// staying the shape they are today.
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Failed to open the login state cookie: %s",
			logging.SanitizeForLog(err.Error()))
		RespondError(w, http.StatusBadRequest, genericCallbackError)
		return nil, false
	}

	// Constant time, not "==": the state value is a secret this server
	// issued, and an early-exit comparison tells anyone who can time the
	// response how much of a guess was right. The comparison happens
	// before the authorization code is read at all, so a forged callback
	// never reaches the token endpoint.
	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state.State)) != 1 {
		log.Printf("[OIDC] Callback state parameter did not match the login state cookie")
		RespondError(w, http.StatusBadRequest, genericCallbackError)
		return nil, false
	}

	return state, true
}

// exchange redeems the authorization code and returns the verified
// identity, having reported any failure to the browser itself.
func (h *OIDCHandler) exchange(w http.ResponseWriter, r *http.Request,
	state *oidc.LoginState) (*oidc.Identity, bool) {

	code := r.URL.Query().Get("code")
	if code == "" {
		log.Printf("[OIDC] Callback carried no authorization code")
		RespondError(w, http.StatusBadRequest, genericCallbackError)
		return nil, false
	}

	// An explicit timeout, on a context derived from the request so that
	// a browser going away still cancels the exchange. Without the
	// timeout, the libraries beneath Exchange use http.DefaultClient,
	// which waits forever.
	ctx, cancel := context.WithTimeout(r.Context(), exchangeTimeout)
	defer cancel()

	identity, err := h.provider.Exchange(ctx, code, state)
	if err != nil {
		// This error can embed the token endpoint's raw response body
		// and claim values from the ID token, so it goes to the log and
		// nowhere else.
		//nolint:gosec // G706: provider-supplied text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Authorization code exchange failed: %s",
			logging.SanitizeForLog(err.Error()))
		redirectTo(w, loginFailedTarget)
		return nil, false
	}

	return identity, true
}

// completeLogin turns a verified identity into a session, applying the
// operator's email domain policy, resolving the Workbench account and
// reconciling its group membership on the way.
func (h *OIDCHandler) completeLogin(w http.ResponseWriter, r *http.Request,
	state *oidc.LoginState, identity *oidc.Identity, ipAddress string) {

	logIdentityDiagnostics(identity)

	if !h.emailDomainAllowed(identity) {
		//nolint:gosec // G706: subject passed through logging.SanitizeForLog
		log.Printf("[OIDC] Refusing subject %s: its email address is not in an allowed domain",
			logging.SanitizeForLog(identity.Subject))
		redirectTo(w, loginFailedTarget)
		return
	}

	federated := federatedIdentity(identity)
	opts := h.federationOptions()

	user, err := h.authStore.ResolveFederatedUser(federated, opts)
	if err != nil {
		// Never switch on this error. Its text distinguishes an unknown
		// subject from a disabled account from a username collision, and
		// the collision case quotes the stored spelling of the local
		// account it collided with.
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Refusing federated login: %s", logging.SanitizeForLog(err.Error()))
		redirectTo(w, loginFailedTarget)
		return
	}

	// A reconciliation failure fails the login. A user whose group
	// membership was only partly applied must not be handed a session:
	// the steps run revocations first, so a partial application can only
	// be missing grants, but a login that silently confers the wrong
	// privileges is worse than one that does not happen.
	if err := h.authStore.ReconcileFederatedGroups(user.ID, federated, opts); err != nil {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Refusing federated login: %s", logging.SanitizeForLog(err.Error()))
		redirectTo(w, loginFailedTarget)
		return
	}

	token, expiration, err := h.authStore.CreateSessionForUser(user.Username)
	if err != nil {
		//nolint:gosec // G706: error text passed through logging.SanitizeForLog
		log.Printf("[OIDC] Refusing federated login: %s", logging.SanitizeForLog(err.Error()))
		redirectTo(w, loginFailedTarget)
		return
	}

	secure := h.isSecureRequest(r)
	// #nosec G124 -- Secure is conditional on isSecureRequest for the
	// same reason as in handleLogin, whose cookie attributes this
	// deliberately matches exactly so that a federated session is
	// indistinguishable from a local one to the browser.
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiration,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// A login that worked gives its allowance back. Without this, the
	// budget is spent by success as readily as by abuse, and on a
	// deployment with no trusted proxy list configured that budget is
	// shared by everyone behind the reverse proxy.
	if h.rateLimiter != nil && ipAddress != "" {
		h.rateLimiter.Reset(ipAddress)
	}

	//nolint:gosec // G706: username passed through logging.SanitizeForLog
	log.Printf("[OIDC] Federated login succeeded for %s", logging.SanitizeForLog(user.Username))

	// state.ReturnPath went through oidc.SanitiseReturnPath when the
	// state was created, and its result is safe only when used exactly
	// as returned: decoding it here, with url.PathUnescape or anything
	// else, would reintroduce the percent-encoded open redirects that
	// function exists to reject.
	redirectTo(w, state.ReturnPath)
}

// logIdentityDiagnostics reports, once per login, what the claim
// extraction had to drop. None of it fails a login, and all of it means
// the operator's provider is sending something the Workbench cannot use,
// which is invisible without this.
func logIdentityDiagnostics(identity *oidc.Identity) {
	if identity.SkippedGroups > 0 {
		//nolint:gosec // G706: subject passed through logging.SanitizeForLog
		log.Printf("[OIDC] Skipped %d unusable value(s) in the groups claim for subject %s",
			identity.SkippedGroups, logging.SanitizeForLog(identity.Subject))
	}
	if identity.UnexpectedGroupsClaimShape != "" {
		//nolint:gosec // G706: both values passed through logging.SanitizeForLog
		log.Printf("[OIDC] Groups claim for subject %s had unexpected shape %s; no groups were read",
			logging.SanitizeForLog(identity.Subject),
			logging.SanitizeForLog(identity.UnexpectedGroupsClaimShape))
	}
	if len(identity.DroppedClaims) > 0 {
		// Names only. DroppedClaims never holds the values, which are
		// the untrusted part, and this must not start logging them.
		//nolint:gosec // G706: both values passed through logging.SanitizeForLog
		log.Printf("[OIDC] Dropped unusable claim(s) %s for subject %s",
			logging.SanitizeForLog(strings.Join(identity.DroppedClaims, ", ")),
			logging.SanitizeForLog(identity.Subject))
	}
}

// emailDomainAllowed applies the operator's allowed_email_domains list.
//
// An empty list means no restriction. A non-empty one means the address
// must be present, must have been verified by the provider, and must sit
// in a listed domain. Absent and rejected addresses are refused rather
// than waved through: Identity.Email is empty both when the provider
// sent nothing and when it sent something unusable, and on a provider
// where users edit their own profile, emptying the field would otherwise
// be a way past the check. An unverified address is refused for the
// same reason in a different costume: it is the user's assertion about
// themselves, not the provider's.
func (h *OIDCHandler) emailDomainAllowed(identity *oidc.Identity) bool {
	if len(h.cfg.AllowedEmailDomains) == 0 {
		return true
	}
	if identity.Email == "" || identity.EmailRejected || !identity.EmailVerified {
		return false
	}

	at := strings.LastIndex(identity.Email, "@")
	if at < 0 || at == len(identity.Email)-1 {
		return false
	}
	domain := identity.Email[at+1:]

	for _, allowed := range h.cfg.AllowedEmailDomains {
		// A leading "@" is trimmed so that an operator who wrote
		// "@example.com" gets what they plainly meant rather than a
		// silent lockout. Comparison is case-insensitive because a
		// domain name is.
		trimmed := strings.TrimPrefix(strings.TrimSpace(allowed), "@")
		if trimmed != "" && strings.EqualFold(trimmed, domain) {
			return true
		}
	}
	return false
}

// federationOptions translates the operator's OIDC configuration into
// the federation policy auth.AuthStore applies. It is the only place the
// two vocabularies meet.
func (h *OIDCHandler) federationOptions() auth.FederationOptions {
	return auth.FederationOptions{
		ProvisionUsers: h.cfg.ProvisionUsersEnabled(),
		GroupMap:       h.cfg.GroupMap,
		SuperuserGroup: h.cfg.SuperuserGroup,
	}
}

// federatedIdentity converts the protocol package's Identity into the
// auth package's FederatedIdentity. The two packages deliberately do not
// import one another, so the conversion lives here, in the one place
// that knows about both.
//
// The diagnostic fields (SkippedGroups, UnexpectedGroupsClaimShape,
// DroppedClaims, EmailRejected) are deliberately not carried across:
// they are for the log, and nothing in the auth store may make a
// decision on them.
func federatedIdentity(identity *oidc.Identity) auth.FederatedIdentity {
	return auth.FederatedIdentity{
		Issuer:      identity.Issuer,
		Subject:     identity.Subject,
		Username:    identity.Username,
		DisplayName: identity.DisplayName,
		Email:       identity.Email,
		Groups:      identity.Groups,
	}
}

// clearStateCookie expires the state cookie. It repeats the Path,
// HttpOnly, Secure and SameSite attributes the cookie was set with,
// because a browser matches on name, domain and path when deciding
// whether one Set-Cookie replaces another, and a mismatch leaves the
// original in place.
func (h *OIDCHandler) clearStateCookie(w http.ResponseWriter, secure bool) {
	// #nosec G124 -- mirrors the flags handleStart set the cookie with;
	// a clear-cookie whose flags differ does not clear anything.
	http.SetCookie(w, &http.Cookie{
		Name:     oidc.StateCookieNameFor(secure),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// allowRequest applies the per-IP rate limit, answering 429 and
// returning false when the caller has run out of allowance.
//
// Every call that gets this far is counted, because the point is to
// bound how often this endpoint can be made to call out to the identity
// provider. A login that completes hands its allowance back again, in
// completeLogin, so that working logins do not spend a budget that,
// without a trusted proxy list, is shared by everyone behind the
// reverse proxy.
func (h *OIDCHandler) allowRequest(w http.ResponseWriter, ipAddress string) bool {
	if h.rateLimiter == nil || ipAddress == "" {
		return true
	}

	if !h.rateLimiter.IsAllowed(ipAddress) {
		RespondError(w, http.StatusTooManyRequests,
			"Too many login requests, please try again later")
		return false
	}
	h.rateLimiter.RecordFailedAttempt(ipAddress)
	return true
}

// extractIPFromRequest returns the client IP for rate limiting, trusting
// forwarded headers only through the configured IPExtractor, which in
// turn trusts them only from configured proxies.
func (h *OIDCHandler) extractIPFromRequest(r *http.Request) string {
	if h.ipExtractor != nil {
		return h.ipExtractor.ExtractIP(r)
	}
	return r.RemoteAddr
}

// isSecureRequest reports whether the request arrived over HTTPS, using
// the shared rule so that the X-Forwarded-Proto trust decision matches
// AuthHandler's exactly.
func (h *OIDCHandler) isSecureRequest(r *http.Request) bool {
	return requestIsSecure(r, h.tlsEnabled, h.ipExtractor)
}

// methodIsGET rejects anything but GET, which is all either endpoint
// ever sees: the browser is redirected to both.
func (h *OIDCHandler) methodIsGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// redirectTo sends a 302 with an empty body.
//
// It does the redirect by hand rather than through http.Redirect because
// the target must reach the Location header exactly as it was computed:
// state.ReturnPath is the output of oidc.SanitiseReturnPath, which is
// safe only when it is neither decoded nor re-encoded, and an empty body
// removes any question of provider-influenced text being reflected into
// one.
func redirectTo(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}
