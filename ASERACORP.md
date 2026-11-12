# aseracorp fork — change ledger

Keeps track of **what this fork changes on top of upstream** [`azukaar/Cosmos-Server`](https://github.com/azukaar/Cosmos-Server), feature by feature, so rebases are mechanical instead of archaeological.

**Current baseline:** upstream `v0.24.0` = `fa0a6523` (42 commits total). Fork adds **6 commits / 20 files** (7 commits incl. this file).

## How to use this during a rebase

```bash
git fetch upstream --prune
git checkout -b rebase/master-<ver> upstream/master
# 1. our net diff vs the OLD merge-base, applied 3-way:
git diff <old-merge-base> backup/pre-rebase-master-YYYYMMDD > /tmp/fork-net.patch
git apply -3 --exclude=.circleci/config.yml /tmp/fork-net.patch
git rm -q --ignore-unmatch .circleci/config.yml docker.sh
# 2. commit in the same 6 groups as below, then run the checklist at the bottom.
```

Do **not** replay old fork commits with `git cherry-pick` — see [History](#history--why-a-plain-rebase-fails).

## The ledger

| # | Feature / fix | Files | Upstream status |
|---|---|---|---|
| F1 | GitHub Actions CI + auto upstream-sync (replaces CircleCI) | `.github/workflows/build.yml`, `.github/workflows/upstream-sync.yml`, −`.circleci/config.yml`, −`docker.sh` | upstream has **no** GH workflows; CircleCI is upstream's release path |
| F2 | Static/musl-safe builds, upx compression | `build.sh`, `build-dev.sh` | **absorbed upstream** (they now set `CGO_ENABLED=0`); our extras: `-trimpath -s -w`, `upx -9`, `-linkmode external` |
| F3 | Canonical `nebula` / `nebula-cert` names on every arch | `src/constellation/nebula.go`, `build.sh`, `build-dev.sh`, 2 tests | **not upstream** (upstream keeps `nebula-arm`/`nebula-arm-cert`) |
| F4 | `iptables -m conntrack --ctstate` (deprecation fix) | `src/constellation/nebula.go` | **not upstream** — v0.24.0 still has `-m state --state` |
| F5 | `shm_size` support (create + export + compose import) | `src/docker/api_blueprint.go`, `src/docker/export.go`, `client/.../docker-compose.jsx`, `go-sdk/client.gen.go` | **not upstream** |
| F6 | OIDC discovery + ID-token issuer from `req.Host` (+ tests) | `src/authorizationserver/oauth2.go`, `oauth2_auth.go`, `oauth2_discover.go`, `oauth2_discover_test.go`, `oauth2_session_test.go` | **not upstream** |
| F7 | Update check points at `aseracorp` | `src/update.go` | **not upstream** |
| F8 | Cosmetic demo-data cleanup | `client/src/api/backup.demo.ts` | n/a (drops 2 `nebula-arm` sample entries, F3 fallout) |

## Feature details + verification anchors

For each feature, the grep that proves it survived a rebase. All expected values verified against the current tree.

### F1 — CI (`ec3871a0`)
- `.github/workflows/build.yml` (216 lines): builds amd64+arm64, publishes GHCR images, GitHub Release `v${VERSION}` (from `package.json`), `.zip` packages. Downloads nebula v1.8.2 + restic v0.17.3, `upx -9`, needs `MAX_TOKEN` secret (MaxMind GeoLite2).
- `.github/workflows/upstream-sync.yml` (137 lines): hourly cron `17 * * * *` for `master` + `unstable`; ancestor check → merge on a `sync-upstream-*` branch → land via direct push (`GH_BOT_TOKEN`) else PR; dispatches `build.yml` after PR merges.
- ⚠️ The commit deletes upstream's release path: `.circleci/config.yml` (237 lines) and `docker.sh` (23 lines). Upstream keeps editing `.circleci/config.yml`, so a rebase always produces a **delete/modify conflict** — resolve by keeping our deletion (`git rm -q .circleci/config.yml docker.sh`).
- ⚠️ `build.yml` runs on **push to `master`/`unstable` only**, and it **publishes a release**. A PR shows 0 check-runs.

### F2 — build flags (`43933f10`)
`build.sh`: `env GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" ...` ×4, then `upx -9 build/*` ×4.
`build-dev.sh`: `CGO_ENABLED=0 go build -ldflags "-linkmode 'external' -extldflags '-static'"`.
- ⚠️ `-linkmode external` **requires cgo**; with `CGO_ENABLED=0` Go errors `-linkmode requires external (cgo) linking, but cgo is not enabled`. Pre-existing on dev builds; `build.yml` uses `build.sh`, so CI is unaffected. Not fixed (out of scope) — if you fix it, drop `-linkmode/-extldflags`.
- anchors: `grep -c trimpath build.sh` = **4** · `grep -c 'upx -9 build/' build.sh` = **4**

### F3 — nebula binary naming (`3fdce7ee` code, `43933f10` scripts)
`binaryToRun()` → `"./nebula"`, `certBinaryToRun()` → `"./nebula-cert"`, no `runtime.GOARCH` branch (removes the `runtime` import usage). `build.sh` copies `nebula-arm64-cert nebula-cert nebula-arm64 nebula`; `build.yml` renames the downloaded arm64 pair into canonical names and keeps the per-arch split at *package* level.
- anchor: `grep -c nebula-arm src/constellation/nebula.go` = **0**
- When rebasing: if upstream re-adds arch-specific names, re-delete the two `if runtime.GOARCH ...` blocks.

### F4 — iptables conntrack (`3fdce7ee`)
In `populateIPTableMasquerade()`, third FORWARD rule:
`iptables -A FORWARD -i %s -o nebula1 -m conntrack --ctstate RELATED,ESTABLISHED ...`
- anchor: `grep -c -- '-m conntrack --ctstate' src/constellation/nebula.go` = **1**
- anchor: `grep -c -- '-m state --state' src/constellation/nebula.go` = **0**
- Upstream v0.24.0 line ~1076 still has the old form → **this fix must be re-applied every rebase** until upstream takes it. Exit nodes break on iptables 1.8+ without it.

### F5 — shm_size (`79d562a7` + `8aadca34`)
- `api_blueprint.go`: `ShmSize int64 \`json:"shm_size,omitempty"\`` in `ContainerCreateRequestContainer` (after `Isolation`), and `ShmSize: container.ShmSize` in `CreateService` (after `Isolation: conttype.Isolation(...)`).
- `export.go`: `ShmSize: detailedInfo.HostConfig.ShmSize` in `ExportContainer` (after `Isolation`).
- `docker-compose.jsx`: converts string `shm_size` (e.g. `"256m"`) to bytes on import via `parse()`. Coexists cleanly with upstream's newer `rootProtocol` state.
- ⚠️ `api-docs/*` **never** documented `shm_size` (adjacent `isolation`/`sysctls` are), so `go-sdk/client.gen.go`'s `ShmSize` fields (2 of them) are a **hand-patch**. `scripts/generate-api.sh` (run by the pre-commit hook) **overwrites** `client.gen.go` and will drop it.
  - anchors: `grep -c shm_size go-sdk/client.gen.go` = **2** · `grep -c shm_size api-docs/openapi.json` = **0**
  - Proper fix (upstream-able): add the swagger annotation so codegen emits it, then the hand-patch disappears.

### F6 — OIDC issuer from request Host (`ae58aecb`)
`discoverEndpoint()` and the `openid.DefaultSession` issuer in `oauth2.go` both resolve via
`utils.FindRouteByReqHost(req.Host)` instead of `utils.GetMainConfig().HTTPConfig.Hostname`, so a host that serves Cosmos (incl. proxy routes) advertises an issuer matching what the client called. `oauth2_auth.go` loses its now-unused `hostname` block (7 lines). `oauth2.go` guards: `if route == nil && hostname != ...HTTPConfig.Hostname { error; return nil }`.
- anchors: `grep -c FindRouteByReqHost src/authorizationserver/oauth2_discover.go` = **1** · `.../oauth2.go` = **2** · `grep -c HTTPConfig.Hostname src/authorizationserver/oauth2_auth.go` = **0**
- **Rebase-critical dependency:** upstream rewrote `src/utils/middleware.go` (+81/−15) between v0.23.4 and v0.24.0. `FindRouteByReqHost` lives in `src/utils/utils.go` (line ~1403) and its signature `(hostname string) (string, *ProxyRouteConfig)` was byte-identical — **verify it on every rebase**: `grep -rn 'func FindRouteByReqHost' src/utils/`
- Tests: `oauth2_discover_test.go`, `oauth2_session_test.go` are **fork-only files** (new files, never conflict).

### F7 — update channel (`ae58aecb`)
`src/update.go`: `updateURL = "https://api.github.com/repos/aseracorp/cosmos-server/releases"` (was `azukaar`). Note the Pro branch (`api.cosmos-cloud.io/proupdates`) is untouched.
- anchor: `grep -c 'aseracorp/cosmos-server/releases' src/update.go` = **1**

## Rebase checklist

```bash
export PATH=/usr/simplify/tools/go/bin:$PATH     # go 1.27.3, matches go.mod
cp src/update.go src/launcher/update.go          # gitignored; build.sh does this
go build -o /tmp/b/cosmos src/*.go                                   # F2
env GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/b/arm64 src/*.go     # F2/F3
go build -o /tmp/b/launcher ./src/launcher/launcher.go ./src/launcher/update.go
go test ./src/...                       # F6 tests, must be 10/10 ok
cd go-sdk && go build ./... && cd ..    # F5 hand-patch compiles
npm install && npm run client-build     # F5 client side
# then every anchor above
```

**Known failures that are NOT regressions** (identical on clean `upstream/master`):
- `go build ./...` → `src/launcher/launcher.go: undefined: GetLatestVersion` (the gitignored `update.go` copy step, see above).
- `go vet ./src/utils/...` → 3 struct-tag warnings in `src/utils/types.go` (265, 269, 597).
- `scripts/pre-commit` (upstream's own, installed by `scripts/install-hooks.sh`) blocks **every** commit on v0.24.0: its regex `isPro\s*:\s*false` rejects upstream's own stub `isPro: ()=> false,`. Fix: `isPro\s*:\s*(\(\)\s*=>\s*)?false`. Workaround for rebasing: `git commit --no-verify` (or `git config core.hooksPath /tmp/nohooks`, then **unset it** — it lives in the shared `.git/config` and silently disables the safety gate for the main clone too).

## History — why a plain rebase fails

Upstream **force-pushed a squashed history**: `upstream/master` is only 42 commits, and our old master sits on upstream's *pre-squash* commits (`merge-base 69c53038` = v0.23.4). So `upstream/master..old-master` = **145 commits**, of which **102 are ghosts** — authored by Yann Stepienik (`azukaar@gmail.com`), i.e. upstream's own discarded history. Only **24 non-merge commits are ours** (+1 merge commit `cc01138a`). A 145-commit `cherry-pick` dies on commit 1 with add/add conflicts across all of `src/`.

**Rule:** rebuild from `upstream/master` + the net diff above. Never merge upstream into a linear fork history, never cherry-pick ghosts.

| ref | what |
|---|---|
| `backup/pre-rebase-master-20261112` | master before the v0.24.0 rebase (`3011b41e`) — source of the net diff |
| PR #51 | `rebase/master-v0.24.0` → master |
| tags `v0.23.4`, `v0.23.1`, `v0.23.0-unstable013`, `v0.22.35` | fall off master (upstream squash); reachable only via the backup branch |

The 25 old fork commits (`a9a7099b`, `48afe423`, `f337db0f`, `e7e5fa81`, `e183377b`, `ef626eae`, `fa06e09f`, `3519a679`, `4d3284ff`, `703c5e3d`, `a6f1295f`, `7041daa8`, `e7455e39`, `f78707c6`, `cb2a88a4`, `7f353d9f`, `81aeb33f`, `529b26d2`, `af638a53`, `bbb0e50a`, `c25c12a0`, `8a26d051`, `cc01138a`, `14536ac4`, `3011b41e`) are superseded by the 6 commits in the table.

*Update this file in the same PR/commit as every rebase — the ledger is only useful if it stays true.*
