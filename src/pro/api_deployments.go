package pro

import (
	"net/http"
	"sync"

	"github.com/azukaar/cosmos-server/src/docker"
	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/nats-io/nats.go"
)


type Deployment struct {
	Name     string                              `json:"name" validate:"required,min=3,max=64,alphanum"`
	Replicas int                                 `json:"replicas" validate:"required,min=1"`
	// Strategy selects which PlacementStrategy the scheduler uses for this
	// deployment. Empty is treated as "round-robin" for back-compat with
	// KV entries written before this field existed.
	Strategy string                              `json:"strategy" validate:"omitempty,oneof=round-robin least-busy"`
	// Tags filter eligible placement nodes. A deployment with Tags=["gpu"]
	// will only land on nodes whose ConstellationDevice.Tags contains "gpu".
	// Multiple tags are AND'd: ["gpu","nvme"] requires both. Empty means no
	// filter — any node is eligible.
	Tags     []string                            `json:"tags,omitempty" validate:"omitempty,dive,min=1,max=64"`
	// Storage lists RCLONE remote names this deployment depends on. Checked
	// node-side in executeApply before docker.CreateService runs — a missing
	// remote produces StatusFail and flows through the existing fail-streak
	// quarantine path. Not a placement filter: RCLONE config is cluster-synced
	// via constellation, so every eligible node has the same remote set.
	// ${storage.NAME} in compose fields resolves to the mount path on apply.
	Storage  []string                            `json:"storage,omitempty" validate:"omitempty,dive,min=1,max=64"`
	Compose  docker.DockerServiceCreateRequest   `json:"compose" validate:"required"`
}


func DeploymentsRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

func DeploymentsIdRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

// listDeployments godoc
// @Summary List all cluster deployments
// @Description Returns all deployment definitions from the constellation-deployments KV (Pro feature)
// @Tags deployments
// @Produce json
// @Security BearerAuth
// @Success 200 {object} utils.APIResponse
// @Failure 401 {object} utils.HTTPErrorResult
// @Failure 403 {object} utils.HTTPErrorResult
// @Router /api/constellation/deployments [get]
func listDeployments(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

// createDeployment godoc
// @Summary Create a new cluster deployment
// @Description Creates a new deployment definition in the constellation-deployments KV (Pro feature)
// @Tags deployments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body Deployment true "Deployment definition"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.HTTPErrorResult
// @Failure 401 {object} utils.HTTPErrorResult
// @Failure 409 {object} utils.HTTPErrorResult
// @Router /api/constellation/deployments [post]
func createDeployment(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

// getDeployment godoc
// @Summary Get a cluster deployment by name
// @Description Returns a single deployment definition (Pro feature)
// @Tags deployments
// @Produce json
// @Security BearerAuth
// @Param name path string true "Deployment name"
// @Success 200 {object} utils.APIResponse
// @Failure 401 {object} utils.HTTPErrorResult
// @Failure 404 {object} utils.HTTPErrorResult
// @Router /api/constellation/deployments/{name} [get]
func getDeployment(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

// updateDeployment godoc
// @Summary Update a cluster deployment
// @Description Updates an existing deployment definition (Pro feature)
// @Tags deployments
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param name path string true "Deployment name"
// @Param body body Deployment true "Updated deployment"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.HTTPErrorResult
// @Failure 401 {object} utils.HTTPErrorResult
// @Failure 404 {object} utils.HTTPErrorResult
// @Router /api/constellation/deployments/{name} [put]
func updateDeployment(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}

// deleteDeployment godoc
// @Summary Delete a cluster deployment
// @Description Removes a deployment from the constellation-deployments KV (Pro feature)
// @Tags deployments
// @Produce json
// @Security BearerAuth
// @Param name path string true "Deployment name"
// @Success 200 {object} utils.APIResponse
// @Failure 401 {object} utils.HTTPErrorResult
// @Failure 404 {object} utils.HTTPErrorResult
// @Router /api/constellation/deployments/{name} [delete]
func deleteDeployment(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
	return
}
