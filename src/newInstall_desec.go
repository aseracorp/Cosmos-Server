package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// DesecSetupRequest is the payload of POST /api/setup/desec.
type DesecSetupRequest struct {
	// One of:
	//  - ExistingToken: reuse an existing deSEC API token (skip account flow)
	//  - Email+Password: register a new deSEC account (email activation required)
	ExistingToken string `json:"existingToken,omitempty"`
	Email         string `json:"email,omitempty"`
	Password      string `json:"password,omitempty"`

	// Captcha: required for new registrations (id+challenge from
	// GET /api/setup/desec/captcha, solution typed by the user).
	CaptchaID       string `json:"captchaId,omitempty"`
	CaptchaSolution string `json:"captchaSolution,omitempty"`

	// DesiredDomain is the domain to create/use, e.g. "mybox.dedyn.io".
	DesiredDomain string `json:"desiredDomain"`
	// Hostname is what Cosmos will serve on (defaults to DesiredDomain).
	Hostname string `json:"hostname,omitempty"`

	// CreateRecords controls whether A/CNAME records are auto-created.
	CreateRecords bool `json:"createRecords"`
}

// DesecSetupResponse is returned by POST /api/setup/desec.
type DesecSetupResponse struct {
	Status          string   `json:"status"` // "pending-activation" | "ok" | "error"
	PendingEmail    string   `json:"pendingEmail,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Hostname        string   `json:"hostname,omitempty"`
	TokenID         string   `json:"tokenId,omitempty"`
	Token           string   `json:"token,omitempty"`
	ActivationState string   `json:"activationState,omitempty"`
	Message         string   `json:"message,omitempty"`
	// For custom (non-dedyn.io) domains: NS records the user must set at the
	// registrar, plus a DNSSEC warning.
	RequiresDelegation bool     `json:"requiresDelegation,omitempty"`
	Nameservers        []string `json:"nameservers,omitempty"`
	DNSSECNote         string   `json:"dnssecNote,omitempty"`
}

// DesecCaptchaResponse is returned by GET /api/setup/desec/captcha.
type DesecCaptchaResponse struct {
	ID        string `json:"id"`
	Challenge string `json:"challenge"` // base64 PNG for <img src="data:image/png;base64,...">
}

// desecSetupState is persisted (in memory) so the wizard can poll activation
// status across requests during a single install session.
var desecSetupState = struct {
	mu              sync.Mutex
	pendingEmail    string
	pendingDomain   string
	pendingPassword string
	pendingCaptcha  string
}{}

// DesecSetupCaptchaRoute handles GET /api/setup/desec/captcha — fetches a
// fresh captcha from deSEC and returns it to the client for display.
// @Summary Get a deSEC registration captcha
// @Description Fetches a fresh captcha from deSEC (id + base64 PNG challenge). The client renders the image and collects the solution, which is sent back with POST /api/setup/desec.
// @Tags system
// @Produce json
// @Router /api/setup/desec/captcha [get]
func DesecSetupCaptchaRoute(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}
	id, challenge, err := utils.DesecGetCaptcha()
	if err != nil {
		utils.HTTPError(w, "desec captcha fetch failed: "+err.Error(), http.StatusBadGateway, "DSEC010")
		return
	}
	json.NewEncoder(w).Encode(DesecCaptchaResponse{ID: id, Challenge: challenge})
}

// DesecSetupRoute handles POST /api/setup/desec — the fully-automatic deSEC
// bootstrap used by the new-install wizard.
// @Summary Auto-provision a deSEC domain + token + records for Cosmos
// @Description Registers/uses a deSEC account, creates a domain and zone records, and returns the token to use for LE DNS-01 + DDNS. Email activation is required for new accounts; a captcha is required to register.
// @Tags system
// @Accept json
// @Produce json
// @Router /api/setup/desec [post]
func DesecSetupRoute(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}

	var request DesecSetupRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		utils.HTTPError(w, "Invalid request: "+err.Error(), http.StatusBadRequest, "DSEC001")
		return
	}

	if request.DesiredDomain == "" {
		utils.HTTPError(w, "desiredDomain is required", http.StatusBadRequest, "DSEC002")
		return
	}
	if request.Hostname == "" {
		request.Hostname = request.DesiredDomain
	}

	// If we already have a token, go straight to config.
	token := strings.TrimSpace(request.ExistingToken)
	if token != "" {
		result, err := desecFinishWithToken(token, request)
		if err != nil {
			utils.HTTPError(w, "desec setup failed: "+err.Error(), http.StatusInternalServerError, "DSEC003")
			return
		}
		json.NewEncoder(w).Encode(result)
		return
	}

	// Account flow: register if not already pending/active.
	if request.Email == "" || request.Password == "" {
		utils.HTTPError(w, "existingToken or email+password required", http.StatusBadRequest, "DSEC004")
		return
	}

	// 1) Attempt to log in FIRST: if the account already exists AND is
	//    activated, login succeeds and we can skip registration + activation
	//    entirely (this also covers the "email already in use" case).
	loginToken, loginErr := utils.DesecLoginToken(request.Email, request.Password)
	if loginErr == nil {
		// Account exists + activated. Finish setup with a fresh scoped token.
		result, err := desecFinishAfterLogin(loginToken, request)
		if err != nil {
			utils.HTTPError(w, "desec setup failed: "+err.Error(), http.StatusInternalServerError, "DSEC009")
			return
		}
		json.NewEncoder(w).Encode(result)
		return
	}

	// 2) No working login yet — attempt registration (may fail if email in
	//    use but pending activation, or if the email is invalid, or the
	//    captcha is wrong — surface the exact deSEC message).
	//    For dedyn.io domains we ask deSEC to create the zone on activation.
	//    For custom domains we skip the domain field here: they are created
	//    later (once the account is active + delegated), because deSEC deletes
	//    the account if a domain in the registration payload cannot be created.
	regDomain := request.DesiredDomain
	if !utils.IsDedynDomain(request.DesiredDomain) {
		regDomain = ""
	}
	registerCode, regErr := utils.DesecRegisterAccount(request.Email, request.Password, regDomain, request.CaptchaID, request.CaptchaSolution)

	// If registration is rejected because the account already exists but is
	// still pending activation (not yet verified), treat as pending-activation.
	if regErr != nil {
		msg := regErr.Error()
		utils.Warn("DesecSetupRoute: register attempt: " + msg)
		if registerCode == http.StatusAccepted || registerCode == http.StatusOK {
			// Registration accepted (202) — email sent.
		} else if registerCode == http.StatusConflict ||
			(strings.Contains(strings.ToLower(msg), "exist") && registerCode >= 400 && registerCode < 500) {
			// Email already registered but not activated, or just registered.
		} else if registerCode >= 400 && registerCode < 500 {
			// Real 4xx: captcha wrong, invalid email/password, etc.
			utils.HTTPError(w, "deSEC registration failed: "+msg, http.StatusBadRequest, "DSEC011")
			return
		} else {
			// 5xx or network error.
			utils.HTTPError(w, "deSEC registration failed: "+msg, http.StatusBadGateway, "DSEC012")
			return
		}
	}

	// Registration accepted or already exists → account requires email
	// activation. Persist state for the poll endpoint.
	desecSetupState.mu.Lock()
	desecSetupState.pendingEmail = request.Email
	desecSetupState.pendingDomain = request.DesiredDomain
	desecSetupState.pendingPassword = request.Password
	desecSetupState.pendingCaptcha = request.CaptchaSolution
	desecSetupState.mu.Unlock()

	// For custom domains, tell the user about delegation requirements up-front.
	resp := DesecSetupResponse{
		Status:          "pending-activation",
		PendingEmail:    request.Email,
		Domain:          request.DesiredDomain,
		Hostname:        request.Hostname,
		ActivationState: "pending",
		Message:         "check your email and click the activation link, then press continue",
	}
	if !utils.IsDedynDomain(request.DesiredDomain) {
		resp.RequiresDelegation = true
		resp.Nameservers = utils.DesecNameservers
		resp.DNSSECNote = "If DNSSEC is enabled on this domain, disable it at the registrar and wait up to 24h for the old DS records to expire before deSEC can serve the zone."
	}
	json.NewEncoder(w).Encode(resp)
}

// DesecSetupStatusRoute handles GET /api/setup/desec/status — polls whether the
// pending account has been activated; if so, completes the bootstrap.
// @Summary Poll deSEC account activation and finish setup
// @Tags system
// @Produce json
// @Router /api/setup/desec/status [get]
func DesecSetupStatusRoute(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}

	desecSetupState.mu.Lock()
	email := desecSetupState.pendingEmail
	domain := desecSetupState.pendingDomain
	password := desecSetupState.pendingPassword
	desecSetupState.mu.Unlock()

	if email == "" || password == "" {
		json.NewEncoder(w).Encode(DesecSetupResponse{Status: "idle"})
		return
	}

	// Try to log in. If it works, the account is activated.
	loginToken, err := utils.DesecLoginToken(email, password)
	if err != nil {
		json.NewEncoder(w).Encode(DesecSetupResponse{
			Status:          "pending-activation",
			PendingEmail:    email,
			Domain:          domain,
			ActivationState: "pending",
			Message:         "activation link not clicked yet",
		})
		return
	}

	// Activated: finish the setup.
	result, err := desecFinishAfterLogin(loginToken, DesecSetupRequest{
		DesiredDomain: domain,
		Hostname:      domain,
		CreateRecords: true,
	})
	if err != nil {
		utils.HTTPError(w, "desec finish setup failed: "+err.Error(), http.StatusInternalServerError, "DSEC006")
		return
	}

	// Clear pending state.
	desecSetupState.mu.Lock()
	desecSetupState.pendingEmail = ""
	desecSetupState.pendingPassword = ""
	desecSetupState.pendingDomain = ""
	desecSetupState.pendingCaptcha = ""
	desecSetupState.mu.Unlock()

	result.Status = "ok"
	json.NewEncoder(w).Encode(result)
}

// desecFinishAfterLogin completes setup after a successful login: mints a
// scoped token, creates the domain + records, wires the config.
func desecFinishAfterLogin(loginToken string, request DesecSetupRequest) (*DesecSetupResponse, error) {
	tokenID, tokenSecret, err := utils.DesecCreateToken(loginToken, "cosmos-"+sanitizeDomain(request.DesiredDomain), true)
	if err != nil {
		return nil, err
	}
	_ = tokenID
	return desecFinishWithToken(tokenSecret, request)
}

// desecFinishWithToken creates the domain + records (if requested), wires the
// token into the Cosmos HTTP config for LE DNS-01, and returns the result.
// For custom domains it also returns the delegation (NS) + DNSSEC guidance.
func desecFinishWithToken(token string, request DesecSetupRequest) (*DesecSetupResponse, error) {
	ctx := context.Background()

	result := &DesecSetupResponse{
		Status:   "ok",
		Domain:   request.DesiredDomain,
		Hostname: request.Hostname,
	}

	// Create domain if it doesn't exist.
	if _, err := utils.DesecCreateDomain(ctx, token, request.DesiredDomain); err != nil {
		// A 400/409 means it already exists — fine.
		utils.Warn("desecFinishWithToken: create domain: " + err.Error())
	}

	if request.CreateRecords {
		// Grab public IP for the apex A record.
		pubIP, _ := utils.GetPublicIPv4()
		if pubIP != "" {
			_ = utils.DesecReplaceRRset(ctx, token, request.DesiredDomain, "", "A", []string{pubIP}, 3600)
		}
		// Wildcard CNAME * -> @.
		_ = utils.DesecReplaceRRset(ctx, token, request.DesiredDomain, "*", "CNAME", []string{request.DesiredDomain + "."}, 3600)
	}

	// Save the token into Cosmos config for LE DNS-01 + DDNS.
	config := utils.ReadConfigFromFile()
	config.HTTPConfig.Hostname = request.Hostname
	config.HTTPConfig.HTTPSCertificateMode = "LETSENCRYPT"
	config.HTTPConfig.UseWildcardCertificate = request.CreateRecords
	config.HTTPConfig.DNSChallengeProvider = "desec"
	if config.HTTPConfig.DNSChallengeConfig == nil {
		config.HTTPConfig.DNSChallengeConfig = map[string]string{}
	}
	config.HTTPConfig.DNSChallengeConfig["DESEC_TOKEN"] = token
	if config.HTTPConfig.SSLEmail == "" {
		config.HTTPConfig.SSLEmail = resultHostEmail(request)
	}

	// Enable DDNS for vpn.<hostname> so the public IP stays current.
	vpnFQDN := "vpn." + strings.TrimPrefix(request.Hostname, "vpn.")
	config.DDNS.Enabled = true
	config.DDNS.FQDN = vpnFQDN
	config.DDNS.Token = token

	utils.SetBaseMainConfig(config)

	// Kick an immediate DDNS update.
	go func() {
		time.Sleep(2 * time.Second)
		utils.DDNSUpdateNow()
	}()

	result.Token = token
	result.ActivationState = "active"

	// Custom domain guidance.
	if !utils.IsDedynDomain(request.DesiredDomain) {
		result.RequiresDelegation = true
		result.Nameservers = utils.DesecNameservers
		result.DNSSECNote = "If DNSSEC is enabled on this domain, disable it at the registrar and wait up to 24h for the old DS records to expire before deSEC can serve the zone."
	}
	return result, nil
}

// sanitizeDomain makes a domain safe to embed in a token name.
func sanitizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.ReplaceAll(d, ".", "-")
	return d
}

// resultHostEmail returns the SSLEmail to use for LE — defaulting to the
// account email if set, else a placeholder.
func resultHostEmail(request DesecSetupRequest) string {
	if request.Email != "" {
		return request.Email
	}
	return "admin@" + strings.TrimPrefix(sanitizeDomain(request.Hostname), "-")
}
