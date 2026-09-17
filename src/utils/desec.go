package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	desec "github.com/nrdcg/desec"
)

// deSEC API endpoint base.
const DesecAPIBase = "https://desec.io/api/v1/"

// DesecAccountInfo mirrors the account fields we need from /auth/account/.
type DesecAccountInfo struct {
	Email       string `json:"email"`
	UserID      string `json:"id"`
	DomainLimit int    `json:"limit_domains"`
}

// DesecSetupResult carries everything produced while bootstrapping a domain.
type DesecSetupResult struct {
	AccountEmail    string   `json:"accountEmail"`
	Domain          string   `json:"domain"`
	Token           string   `json:"token"`
	TokenID         string   `json:"tokenId"`
	ActivationState string   `json:"activationState"` // pending | active
	RRsetsCreated   []string `json:"rrsetsCreated"`
}

var desecHTTPClient = &http.Client{Timeout: 30 * time.Second}

// desecRequest performs a raw deSEC API call (used for endpoints not covered
// by the nrdcg/desec client, e.g. account registration/activation).
func desecRequest(method, path string, token string, payload interface{}, out interface{}) (int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, DesecAPIBase+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}

	resp, err := desecHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if out != nil {
		raw, _ := io.ReadAll(resp.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, out)
		}
	}
	return resp.StatusCode, nil
}

// DesecCaptcha is the {id, challenge} pair returned by POST /captcha/.
type DesecCaptcha struct {
	ID        string `json:"id"`
	Challenge string `json:"challenge"` // base64-encoded PNG
}

// DesecGetCaptcha fetches a fresh captcha from deSEC. The challenge is a
// base64-encoded PNG that the client renders as data:image/png;base64,<ch>.
// The user must type the visible characters; the solution + id are then sent
// with the registration request (or, if omitted at registration, they are
// required later when completing email activation).
func DesecGetCaptcha() (string, string, error) {
	var out DesecCaptcha
	code, err := desecRequest("POST", "captcha/", "", nil, &out)
	if err != nil {
		return "", "", err
	}
	if code != http.StatusCreated && code != http.StatusOK {
		return "", "", fmt.Errorf("desec captcha: status %d", code)
	}
	if out.ID == "" || out.Challenge == "" {
		return "", "", fmt.Errorf("desec captcha: empty response")
	}
	return out.ID, out.Challenge, nil
}

// DesecRegisterAccount creates a deSEC account. If a domain name is given it
// is created upon activation. Registration returns immediately (202) and the
// account must be activated via the emailed link before it can be used.
// Optional captchaID/solution can be provided; when omitted deSEC requires
// them later at email-activation time. Returns the HTTP status code.
func DesecRegisterAccount(email, password, desiredDomain, captchaID, captchaSolution string) (int, error) {
	payload := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	if desiredDomain != "" {
		payload["domain"] = desiredDomain
	}
	if captchaID != "" && captchaSolution != "" {
		payload["captcha"] = map[string]string{
			"id":       captchaID,
			"solution": captchaSolution,
		}
	}
	code, err := desecRequest("POST", "auth/", "", payload, nil)
	if err != nil {
		return 0, fmt.Errorf("desec register: %w", err)
	}
	return code, nil
}



// DesecLoginToken returns a short-lived login token (for machine-setup use
// only; the long-lived scoped token is minted separately via
// DesecCreateToken).
func DesecLoginToken(email, password string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	code, err := desecRequest("POST", "auth/login/", "", map[string]string{
		"email":    email,
		"password": password,
	}, &out)
	if err != nil {
		return "", err
	}
	if code != http.StatusOK || out.Token == "" {
		return "", fmt.Errorf("desec login: status %d", code)
	}
	return out.Token, nil
}

// DesecCreateToken creates a new API token with explicit permissions. If
// createDomain is true the token can create/delete domains – required for the
// auto-setup flow (registering the dedyn.io zone). Called with an account
// login token (or an existing privileged API token).
func DesecCreateToken(authToken, name string, createDomain bool) (string, string, error) {
	payload := map[string]interface{}{
		"name": name,
	}
	if createDomain {
		payload["perm_create_domain"] = true
		payload["perm_delete_domain"] = true
	}
	var out struct {
		ID               string `json:"id"`
		Token            string `json:"token"`
		PermCreateDomain bool   `json:"perm_create_domain"`
	}
	code, err := desecRequest("POST", "auth/tokens/", authToken, payload, &out)
	if err != nil {
		return "", "", err
	}
	if code != http.StatusCreated || out.Token == "" {
		return "", "", fmt.Errorf("desec create token: status %d", code)
	}
	return out.ID, out.Token, nil
}

// DesecCreateDomain registers a domain (requires a token with domain-create
// permission).
func DesecCreateDomain(ctx context.Context, token, domain string) (*desec.Domain, error) {
	client := desec.New(token, desec.NewDefaultClientOptions())
	d, err := client.Domains.Create(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("desec create domain: %w", err)
	}
	return d, nil
}

// DesecCreateRRset creates a DNS record (RRset) on a domain.
func DesecCreateRRset(ctx context.Context, token, domain string, rr desec.RRSet) (*desec.RRSet, error) {
	client := desec.New(token, desec.NewDefaultClientOptions())
	rr.Domain = domain
	out, err := client.Records.Create(ctx, rr)
	if err != nil {
		return nil, fmt.Errorf("desec create rrset: %w", err)
	}
	return out, nil
}

// DesecReplaceRRset replaces an existing RRset (idempotent create-or-replace).
func DesecReplaceRRset(ctx context.Context, token, domain, subname, rtype string, records []string, ttl int) error {
	client := desec.New(token, desec.NewDefaultClientOptions())
	rr := desec.RRSet{
		Domain:  domain,
		SubName: subname,
		Type:    rtype,
		Records: records,
		TTL:     ttl,
	}
	_, err := client.Records.Replace(ctx, domain, subname, rtype, rr)
	if err != nil {
		return fmt.Errorf("desec replace rrset: %w", err)
	}
	return nil
}

// DesecDeleteRRset removes an RRset.
func DesecDeleteRRset(ctx context.Context, token, domain, subname, rtype string) error {
	client := desec.New(token, desec.NewDefaultClientOptions())
	if err := client.Records.Delete(ctx, domain, subname, rtype); err != nil {
		return fmt.Errorf("desec delete rrset: %w", err)
	}
	return nil
}

// DesecDynDNSUpdate updates the A/AAAA record of fqdn to the given IPv4 (and
// optionally IPv6) via the deSEC dynDNS endpoint. Empty ipv6 preserves the
// existing AAAA record. The token is sent in the Authorization header, never
// in the query string.
func DesecDynDNSUpdate(fqdn, token, ipv4, ipv6 string) error {
	q := url.Values{}
	q.Set("hostname", fqdn)
	if ipv4 != "" {
		q.Set("myipv4", ipv4)
	}
	if ipv6 != "" {
		q.Set("myipv6", ipv6)
	} else {
		q.Set("myipv6", "preserve")
	}
	endpoint := "https://update.dedyn.io/?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+token)

	resp, err := desecHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("desec dyndns: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "good" {
		return fmt.Errorf("desec dyndns: status %d body %q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// DesecNameservers lists deSEC's name servers that a custom (non-dedyn.io)
// domain must be delegated to at the registrar. Most registrars accept the
// hostnames directly as NS records.
var DesecNameservers = []string{"ns1.desec.io", "ns2.desec.io", "ns3.desec.io", "ns4.desec.io"}

// IsDedynDomain returns true if the domain is one deSEC offers for direct
// registration (a single label under dedyn.io), which requires no external
// delegation. Custom domains (e.g. example.com) must be delegated to
// DesecNameservers at the registrar before deSEC can serve them.
func IsDedynDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain, ".")))
	if !strings.HasSuffix(domain, ".dedyn.io") {
		return false
	}
	label := strings.TrimSuffix(domain, ".dedyn.io")
	return label != "" && !strings.Contains(label, ".")
}
