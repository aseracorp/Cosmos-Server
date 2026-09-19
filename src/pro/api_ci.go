// Community build stub of the Cosmos Pro feature set; handlers answer PRO001.

package pro

import (
	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/nats-io/nats.go"
	"net/http"
	"sync"
)

// CIProjectsRoute lists (GET) or creates (POST) projects.
// @Router /api/constellation/ci/projects [get]
// @Router /api/constellation/ci/projects [post]
func CIProjectsRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIProjectsIdRoute reads (GET), replaces (PUT) or deletes (DELETE) a project.
// @Param name path string true "Project name"
// @Router /api/constellation/ci/projects/{name} [get]
// @Router /api/constellation/ci/projects/{name} [put]
// @Router /api/constellation/ci/projects/{name} [delete]
func CIProjectsIdRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIProjectWebhookRoute rotates the secret (POST .../webhook/rotate) or
// re-registers the hook on the provider (POST .../webhook/register).
// @Param name path string true "CI project name"
// @Param action path string true "Action"
// @Router /api/constellation/ci/projects/{name}/webhook/{action} [post]
func CIProjectWebhookRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIDetectRoute clones a repository and reports what CI would do with it.
// Body: {"source": CISource, "build": CIBuildSettings}.
// @Router /api/constellation/ci/detect [post]
func CIDetectRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIBuildsRoute lists (GET, ?limit=) or triggers (POST {branch, sha}) builds
// of a project.
// @Param name path string true "CI project name"
// @Router /api/constellation/ci/projects/{name}/builds [get]
// @Router /api/constellation/ci/projects/{name}/builds [post]
func CIBuildsRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIAllBuildsRoute lists recent builds across projects (?limit=).
// @Router /api/constellation/ci/builds [get]
func CIAllBuildsRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIBuildIdRoute reads one build (GET) or deletes its record (DELETE).
// @Param name path string true "CI project name"
// @Param number path string true "Build number"
// @Router /api/constellation/ci/projects/{name}/builds/{number} [get]
// @Router /api/constellation/ci/projects/{name}/builds/{number} [delete]
func CIBuildIdRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIBuildActionRoute acts on a build: cancel | retry | approve | deploy.
// @Param name path string true "CI project name"
// @Param number path string true "Build number"
// @Param action path string true "Action"
// @Router /api/constellation/ci/projects/{name}/builds/{number}/{action} [post]
func CIBuildActionRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIBuildLogsRoute returns a step's output from chunk `from`
// (?step=<index>&from=<chunk>). The answer carries `next`, the chunk to ask
// for on the next poll, and `done` when the step has finished.
// @Param name path string true "CI project name"
// @Param number path string true "Build number"
// @Router /api/constellation/ci/projects/{name}/builds/{number}/logs [get]
func CIBuildLogsRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIRunnersRoute reports the build capacity of the cluster: every node with
// its running builds and buildkitd state.
// @Router /api/constellation/ci/runners [get]
func CIRunnersRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}

// CIWebhookRoute godoc
// @Summary Receive a CI provider webhook
// @Description Accepts a push / pull-request delivery for the named project and queues a build (Pro feature)
// @Tags ci
// @Accept json
// @Produce json
// @Param name path string true "CI project name"
// @Success 200 {object} utils.APIResponse
// @Router /api/constellation/ci/hooks/{name} [post]
func CIWebhookRoute(w http.ResponseWriter, req *http.Request, lock *sync.RWMutex, js nats.JetStreamContext) {
	utils.Error("This is a pro and is not currently available on your server. Please upgrade to Cosmos Pro to access this feature.", nil)
	utils.HTTPError(w, "This feature is only available in Cosmos Pro", http.StatusForbidden, "PRO001")
}
