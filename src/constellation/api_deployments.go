package constellation

import (
	"net/http"

	"github.com/azukaar/cosmos-server/src/pro"
)

func DeploymentsRoute(w http.ResponseWriter, req *http.Request) {
	pro.DeploymentsRoute(w, req, &clientConfigLock, js)
}

func DeploymentsIdRoute(w http.ResponseWriter, req *http.Request) {
	pro.DeploymentsIdRoute(w, req, &clientConfigLock, js)
}
