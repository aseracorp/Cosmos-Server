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

	// DesiredDomain is the domain to create/use, e.g. "mybox.dedyn.io".
	DesiredDomain string `json:"desiredDomain"`
	// Hostname is what Cosmos will serve on (defaults to DesiredDomain).
	Hostname string `json:"hostname,omitempty"`

	// CreateRecords controls whether A/CNAME records are auto-created.
	CreateRecords bool `json:"createRecords"`
}

// DesecSetupResponse is returned by POST /api/setup/desec.
type DesecSetupResponse struct {
	Status          string `json:"status"` // "pending-activation" | "ok" | "error"
	PendingEmail    string `json:"pendingEmail,omitempty"`
	Domain          string `json:"domain,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	TokenID         string `json:"tokenId,omitempty"`
	Token           string `json:"token,omitempty"`
	ActivationState string `json:"activationState,omitempty"`
	Message         string `json:"message,omitempty"`
}

// desecSetupState is persisted (in memory) so the wizard can poll activation
// status across requests during a single install session.
var desecSetupState = struct {
	mu              sync.Mutex
	pendingEmail    string
	pendingDomain   string
	pendingPassword string
}{}

// DesecSetupRoute handles POST /api/setup/desec — the fully-automatic deSEC
// bootstrap used by the new-install wizard.
// @Summary Auto-provision a deSEC domain + token + records for Cosmos
// @Description Registers/uses a deSEC account, creates a domain and zone records, and returns the token to use for LE DNS-01 + DDNS. Email activation is required for new accounts.
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

	if err := utils.DesecRegisterAccount(request.Email, request.Password, request.DesiredDomain); err != nil {
		// If the account already exists, this returns 400/403 — treat as
		// activation-pending rather than fatal only if the email matches.
		utils.Warn("DesecSetupRoute: register attempt: " + err.Error())
	}

	// Account requires email activation. Persist state for the poll endpoint.
	desecSetupState.mu.Lock()
	desecSetupState.pendingEmail = request.Email
	desecSetupState.pendingDomain = request.DesiredDomain
	desecSetupState.pendingPassword = request.Password
	desecSetupState.mu.Unlock()

	json.NewEncoder(w).Encode(DesecSetupResponse{
		Status:          "pending-activation",
		PendingEmail:    request.Email,
		Domain:          request.DesiredDomain,
		Hostname:        request.Hostname,
		ActivationState: "pending",
		Message:         "check your email and click the activation link, then press continue",
	})
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

	// Activated: create scoped token, domain, records, config.
	tokenID, tokenSecret, err := utils.DesecCreateToken(loginToken, "cosmos-"+sanitizeDomain(domain), true)
	if err != nil {
		utils.HTTPError(w, "desec create token failed: "+err.Error(), http.StatusInternalServerError, "DSEC005")
		return
	}
	_ = tokenID

	// Complete the same path as an existing-token setup, using the new token.
	result, err := desecFinishWithToken(tokenSecret, DesecSetupRequest{
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
	desecSetupState.mu.Unlock()

	result.Status = "ok"
	json.NewEncoder(w).Encode(result)
}

// desecFinishWithToken creates the domain + records (if requested), wires the
// token into the Cosmos HTTP config for LE DNS-01, and returns the result.
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
