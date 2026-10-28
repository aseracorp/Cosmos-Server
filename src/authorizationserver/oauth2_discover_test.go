package authorizationserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/azukaar/cosmos-server/src/utils"
)

// TestDiscoverEndpointUsesRequestHost ensures the OIDC discovery document builds
// every endpoint from the host the client used to reach Cosmos (req.Host), not
// from the configured main hostname. When Cosmos is installed with a private IP
// or an unreachable hostname (e.g. 10.10.10.157) and a client connects through
// an alternate name (Tailscale hostname, reverse proxy, custom domain), the old
// code advertised an authorization_endpoint pointing at the private IP while the
// issuer pointed at the request host — breaking OIDC clients such as Home
// Assistant.
func TestDiscoverEndpointUsesRequestHost(t *testing.T) {
	cfg := utils.DefaultConfig
	cfg.HTTPConfig.Hostname = "10.10.10.157"
	utils.SetBaseMainConfig(cfg)
	utils.IsHTTPS = true

	req := httptest.NewRequest("GET", "https://cosmos-cloud-ebc1/.well-known/openid-configuration", nil)
	req.Host = "cosmos-cloud-ebc1"
	rw := httptest.NewRecorder()

	discoverEndpoint(rw, req)

	if rw.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}

	var doc struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		JWKsURI               string `json:"jwks_uri"`
		UserinfoEndpoint      string `json:"userinfo_endpoint"`
		EndSessionEndpoint    string `json:"end_session_endpoint"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &doc); err != nil {
		t.Fatalf("failed to decode discovery doc: %v", err)
	}

	if doc.Issuer != "https://cosmos-cloud-ebc1" {
		t.Errorf("issuer = %q, want https://cosmos-cloud-ebc1", doc.Issuer)
	}
	if doc.AuthorizationEndpoint != "https://cosmos-cloud-ebc1/cosmos-ui/openid" {
		t.Errorf("authorization_endpoint = %q, want https://cosmos-cloud-ebc1/cosmos-ui/openid", doc.AuthorizationEndpoint)
	}
	if doc.TokenEndpoint != "https://cosmos-cloud-ebc1/oauth2/token" {
		t.Errorf("token_endpoint = %q, want https://cosmos-cloud-ebc1/oauth2/token", doc.TokenEndpoint)
	}
	if doc.JWKsURI != "https://cosmos-cloud-ebc1/.well-known/jwks.json" {
		t.Errorf("jwks_uri = %q, want https://cosmos-cloud-ebc1/.well-known/jwks.json", doc.JWKsURI)
	}
	if doc.UserinfoEndpoint != "https://cosmos-cloud-ebc1/oauth2/userinfo" {
		t.Errorf("userinfo_endpoint = %q, want https://cosmos-cloud-ebc1/oauth2/userinfo", doc.UserinfoEndpoint)
	}
	if doc.EndSessionEndpoint != "https://cosmos-cloud-ebc1/cosmos-ui/logout" {
		t.Errorf("end_session_endpoint = %q, want https://cosmos-cloud-ebc1/cosmos-ui/logout", doc.EndSessionEndpoint)
	}
}