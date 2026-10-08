package configapi

import (
	"encoding/json"
	"net/http"

	"github.com/azukaar/cosmos-server/src/utils"
)

// DDNSRoute handles /api/ddns — GET returns current DDNS state, POST updates
// the config and triggers an immediate update.
// @Summary Manage deSEC DDNS configuration
// @Tags config
// @Security BearerAuth
func DDNSRoute(w http.ResponseWriter, req *http.Request) {
	if utils.CheckPermissions(w, req, utils.PERM_CONFIGURATION) != nil {
		return
	}

	switch req.Method {
	case "GET":
		config := utils.GetMainConfig()
		out := config.DDNS
		if !utils.HasPermission(req, utils.PERM_CREDENTIALS_READ) {
			out.Token = "***"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "OK",
			"data":   out,
		})
	case "POST":
		var request struct {
			Enabled bool   `json:"enabled"`
			FQDN    string `json:"fqdn"`
			Token   string `json:"token"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			utils.HTTPError(w, "Invalid request: "+err.Error(), http.StatusBadRequest, "DDNS001")
			return
		}
		config := utils.ReadConfigFromFile()
		config.DDNS.Enabled = request.Enabled
		config.DDNS.FQDN = request.FQDN
		if request.Token != "" && request.Token != "***" {
			config.DDNS.Token = request.Token
		}
		utils.SetBaseMainConfig(config)

		// Trigger an immediate update so the user sees the result right away.
		utils.DDNSUpdateNow()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "OK",
		})
	default:
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
	}
}

// NetworkDetectRoute handles GET /api/network/detect — runs public-IP/CGNAT
// detection and returns the result, including UPnP availability and router
// vendor info.
// @Summary Detect public IP / CGNAT / router vendor
// @Tags config
// @Security BearerAuth
func NetworkDetectRoute(w http.ResponseWriter, req *http.Request) {
	if utils.CheckPermissions(w, req, utils.PERM_CONFIGURATION_READ) != nil {
		return
	}
	if req.Method != "GET" {
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}
	info := utils.DetectNetwork()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "OK",
		"data":   info,
	})
}

// UPnPRoute handles POST /api/network/upnp — add or remove the Constellation
// UDP 4242 port mapping.
// @Summary Add/remove UPnP port mapping for Constellation
// @Tags config
// @Security BearerAuth
func UPnPRoute(w http.ResponseWriter, req *http.Request) {
	if utils.CheckPermissions(w, req, utils.PERM_CONFIGURATION) != nil {
		return
	}
	if req.Method != "POST" {
		utils.HTTPError(w, "Method not allowed", http.StatusMethodNotAllowed, "HTTP001")
		return
	}
	var request struct {
		Action string `json:"action"` // "add" | "remove"
	}
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		utils.HTTPError(w, "Invalid request: "+err.Error(), http.StatusBadRequest, "UPNP001")
		return
	}
	switch request.Action {
	case "add":
		if err := utils.AddUPnPPortMapping(); err != nil {
			utils.HTTPError(w, "UPnP add failed: "+err.Error(), http.StatusInternalServerError, "UPNP002")
			return
		}
	case "remove":
		if err := utils.RemoveUPnPPortMapping(); err != nil {
			utils.HTTPError(w, "UPnP remove failed: "+err.Error(), http.StatusInternalServerError, "UPNP003")
			return
		}
	default:
		utils.HTTPError(w, "unknown action", http.StatusBadRequest, "UPNP004")
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "OK",
	})
}