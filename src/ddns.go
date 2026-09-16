package main

import (
	"github.com/azukaar/cosmos-server/src/utils"
)

// DDNSUpdateNow triggers a single immediate DDNS update (delegates to utils so
// the settings API and installer can call it from any package). Safe to call
// concurrently.
func DDNSUpdateNow() bool {
	return utils.DDNSUpdateNow()
}

// DDNSWorker is the periodic scheduler entry point (runs every 10 minutes via
// CRON.go). It performs an update only when the public IP changed.
func DDNSWorker() {
	utils.DDNSUpdateNow()
}
