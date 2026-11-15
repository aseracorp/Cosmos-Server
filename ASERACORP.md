# aseracorp fork — change ledger (`unstable`)

What this fork changes on top of upstream [`azukaar/Cosmos-Server`](https://github.com/azukaar/Cosmos-Server), feature by feature, so rebases are mechanical instead of archaeological. Most of it exists as an **upstream PR** — linked below, with its live status. `master` has its own ledger ([`ASERACORP.md` on `master`](https://github.com/aseracorp/Cosmos-Server/blob/master/ASERACORP.md)); this one tracks the `unstable` channel.

**Current baseline:** upstream `unstable` = `5ee16623` = **v0.24.0-unstable031**. Fork adds **39 commits / 132 files** (+12261 / −4036): one commit per feature/fix, landed on `unstable` by force-push (history rewrite), PR #52.

## Upstream PR status at a glance

43 PRs opened from this fork (`aseracorp` + `madejackson`): **1 merged** (#569), **29 open**, **13 closed-unmerged**. Open PRs are the rebase's safety net — re-check them at every rebase, they can be re-opened/rebased against a newer upstream.

## How to use this during a rebase

```bash
git fetch upstream --prune
git checkout -b rebase/unstable-v0.31 upstream/unstable
# replay the fork's own commits (they are all real fork commits, no ghosts here):
git cherry-pick <oldest>^..<old-fork-tip>      # = upstream/unstable..backup/pre-rebase-unstable-<date>
```

Unlike `master`, `unstable` **can** be cherry-picked: the merge-base is recent (`27f7f447` = v0.24.0-unstable030), so the fork's commits sit on top of real upstream commits. The `master` "never cherry-pick, rebuild from net diff" rule exists only because upstream squash-force-pushed `master` (see the master ledger).

History on `unstable` is **one commit per feature/fix** (43 raw fork commits → 39: the 3 `shm_size` commits and the 3-commit OIDC chain each squashed to one, the `hostChip.jsx` rebase fixup folded into the health-badges commit, and the `48c0143e` merge-damage repair split into probe-middleware + export-repair). Replaying the raw range reproduces the same tree; keep that invariant and the net-diff check stays valid.

Then verify with the **net-diff equality check** (the real test, not "it compiles"):

```bash
MB=$(git merge-base <old-fork-tip> upstream/unstable)
git diff --numstat $MB <old-fork-tip>  | sort > /tmp/old.txt
git diff --numstat upstream/unstable HEAD | sort > /tmp/new.txt
# file lists must be identical; per-file +/- counts identical except for keys upstream itself deleted
```

This rebase: 132 files = 132 files, **zero** symmetric difference, per-file line counts identical on **131/132** files.

## The ledger — fork commits → upstream PRs

Verified by **patch-id** (`git show <fork commit> | git patch-id`) matched against every commit inside each upstream PR head (`refs/pull/<N>/head`), plus commit-subject matching where a PR rebased/amended the commit. `open` = PR still open upstream, i.e. the change is **not** in upstream yet.

| Fork commit | Change | Upstream PR | Status |
|---|---|---|---|
| `dce5647d` | `ci(release)`: restore fork build/publish workflow (`build.yml`) | — (fork infra; see #444, #537 for the upstream-able build parts) | closed |
| `5daf61f3` | `fix`: store admin email during stepped new-install setup (bad struct tag `json:"omitempty,email"`) | [#606](https://github.com/azukaar/Cosmos-Server/pull/606) (also #604, #605) | open |
| `1ac61ed9` | `fix(ui)`: container log timestamp extraction anchored + dedup | [#603](https://github.com/azukaar/Cosmos-Server/pull/603) | open |
| `86f01d5a` | `fix(ui)`: licence key never breaks at hyphens | [#602](https://github.com/azukaar/Cosmos-Server/pull/602) | open |
| `151bfe92` | `fix(ui)`: restore ServApp action button tooltips | [#601](https://github.com/azukaar/Cosmos-Server/pull/601) | open |
| `b419e076` | `fix(ui)`: decode URL-encoded segments in breadcrumbs | [#600](https://github.com/azukaar/Cosmos-Server/pull/600) | open |
| `8fc5e1ff` | `fix(ui)`: web terminal ghost key names (AltGraph, Process, Unidentified) | [#598](https://github.com/azukaar/Cosmos-Server/pull/598) | open |
| `f5241489` | `fix(ui)`: guard market detail render when app not found | [#597](https://github.com/azukaar/Cosmos-Server/pull/597) | open |
| `dd395ed2` | `fix(http)`: restore HTTP→HTTPS redirect handler ⚠️ **fork-owned divergence vs upstream's `zones_test.go` — decided, see Conflicts** | [#576](https://github.com/azukaar/Cosmos-Server/pull/576) | open |
| `61703829` | `fix(docker)`: healthcheck durations as compose-style strings | [#585](https://github.com/azukaar/Cosmos-Server/pull/585) | open |
| `f1547c0b` | `fix(docker)`: accept string-form healthcheck `test:` in compose imports | [#595](https://github.com/azukaar/Cosmos-Server/pull/595) | open |
| `c0059e5e` | `shm_size`: support + compose import + byte-size string form — **one commit** (was 3: `34d247d8`/`e2c8431b`/`db6171c5`) | [#567](https://github.com/azukaar/Cosmos-Server/pull/567) | open |
| `14fe453f` | `feat(market)`: lazily generate Passwords/Secrets from template refs | [#586](https://github.com/azukaar/Cosmos-Server/pull/586) | open |
| `1246cdc7` | `fix(mounts)`: normalize volume field casing + back-compat | [#573](https://github.com/azukaar/Cosmos-Server/pull/573) | open |
| `9a3c6c4d` | `feat(docker)`: end-to-end `depends_on` with compose parity | [#579](https://github.com/azukaar/Cosmos-Server/pull/579) | open |
| `9eac3a8d` | `feat(docker)`: volume mount `readOnly`, subpath, volume-nocopy | [#599](https://github.com/azukaar/Cosmos-Server/pull/599) | open |
| `b2ba0df7` | `feat(ui)`: display/edit JSON payloads as HJSON | [#584](https://github.com/azukaar/Cosmos-Server/pull/584) | open |
| `901f55b6` | `feat(cron)`: standard 5-field crontabs + schedule validation | [#596](https://github.com/azukaar/Cosmos-Server/pull/596) | open |
| `60cc9093` | `feat(docker)`: user-set vs runtime config + persist HJSON comments | [#590](https://github.com/azukaar/Cosmos-Server/pull/590) | open |
| `dffff580` | `fix(servapps)`: HostChip status dot reads the real HTTP status | [#582](https://github.com/azukaar/Cosmos-Server/pull/582) (needs #580 for COOP/COEP/CORP) | open / #580 closed |
| `52ffdf7d` | `fix(ui)`: valid input type for nickname (Bitwarden Android autofill) | [#575](https://github.com/azukaar/Cosmos-Server/pull/575) (#574 = same on `master`) | open |
| `9b9e8605` | `fix(ui)`: OIDC route name without `__route_` prefix | [#570](https://github.com/azukaar/Cosmos-Server/pull/570) | open |
| `c9c87efe` | build size: `-trimpath`, `-s -w`, `upx -9` | [#565](https://github.com/azukaar/Cosmos-Server/pull/565) | open |
| `3fc0ac81` | `feat(servapps)`: health-aware status badges (absorbs the `hostChip.jsx` conflict-label fixup `edf89b51`) | [#581](https://github.com/azukaar/Cosmos-Server/pull/581) | open |
| `685a7ede` | `fix(docker)`: start recreated container after update (unless stopped/lazy) | [#620](https://github.com/azukaar/Cosmos-Server/pull/620) (re-taken as #617→#618→#619) | open |
| `45fe47b4` | `fix(oidc)`: host-agnostic discovery + issuer from the same host lookup — **one commit** (was the #41/#46/#48 chain: `1c0a90a2`/`efb9c318`/`0347ae00`) | [#627](https://github.com/azukaar/Cosmos-Server/pull/627) — the consolidated version (the pre-squash tip commit was a **byte-identical patch-id match**) | open |
| `bffc0766` | `fix(docker)`: merge top-level `Mounts` so anonymous image volumes survive export | part of the [#599](https://github.com/azukaar/Cosmos-Server/pull/599) mount work (no standalone PR) | open |
| `03849c55` | `fix(http)`: don't fatal on empty inapplicable TLS cert pairs at first setup | **none** — fork-only, needs a PR | — |
| `7bc9c2e6` | `fix(lazy)`: derive dormant from upstream semantics, drop persisted `cosmos-lazy-dormant` label (fork #30) | **none** — fork-only (touches `lazy.go`, which #581 also edits) | — |
| `bd69b01c` | `fix(config)`: atomic config writes + serialised read/modify/write (fork #37) | **none** — fork-only, needs a PR | — |
| `c12adfec` | `feat(proxy)`: per-route search-engine indexing policy (fork #38) | **none** — fork-only, needs a PR | — |
| `e89f5389` `1eb6e645` | `fix(docker)`: servapp memory/CPU limits apply + stick; expose swap/soft limits (fork #49, #50) | **none** — fork-only, needs a PR (the merged #569 "zram/swap under disks" is a *different* feature) | — |
| `924f8eb3` | `fix(proxy)`: `headProbeWriter`/`HeadProbeCORS`/`originUnderDomain` middleware for the `__cosmos_probe` HEAD probe (half of old `48c0143e`) | **none** — fork-only, needs a PR (pairs with #580 COOP/COEP) | — |
| `487fe7c0` | `fix(build)`: repair compose-export/blueprint merge damage (other half of old `48c0143e`; repairs #10/#594/#580 work) | **none** — fork-only repair | — |
| `c15b1b2c` | `fix(build)`: correct Pro-leak guard regex in `scripts/pre-commit` | **none** — upstream's own hook, see **Traps** | — |
| `3e6e4093` | `chore(update)`: check updates on `aseracorp` (fork #44) | **none** — deliberate fork identity, keep out of upstream | — |
| `fe1f2d1d` | `chore`: regenerate `api-docs/` + Go SDK (follows #590/#599) | — (chore) | — |
| branch `feat/socks5-relay-routes` (`1eb395f2` relay, `f05e2de1` editor, `b0b2022b` wire format, `c3a6857e` e2e tests, `e9319dbb` TCP-only, `6835f36a` + `9e3d30ed` docs/ledger, `7f002a3a` socks5h/remote-DNS dropped, `4ab0fb4b` reply framing fix + domain-refusal e2e) | `feat(proxy)`: SOCKS5 relay routes — `socks5` target scheme only, RFC 1928 CONNECT + RFC 1929 auth, account-backed credentials, IP/CIDR destination allow/deny, TCP-only (UDP ASSOCIATE, BIND and ATYP=domain refused — no outbound resolution, this is a reverse proxy); editor section + wire contract tests | [#58](https://github.com/aseracorp/Cosmos-Server/pull/58) — open against **fork** `unstable` | — (on branch, not landed on `unstable` yet) |

Fork contributions **already merged** upstream: [#569](https://github.com/azukaar/Cosmos-Server/pull/569) (fix zram/swap shown under disks).

## Conflicts the fork introduces (not rebase damage — decide on purpose)

### `plainHTTPHandler`: fork 301 vs upstream 404 — **decided, fork keeps the redirect**

This was the one test failing on the rebased tree:

```
zones_test.go:182: http://www.own.org answered 301, want 404
zones_test.go:182: http://other.net answered 301, want 404
zones_test.go:182: http://192.168.1.10 answered 301, want 404
```

- **Not a rebase regression.** It failed identically at the old fork tip `0d1620ff`, and **passes** on clean `upstream/unstable` (`5ee16623`).
- **Cause:** upstream added `src/zones_test.go` in `63e5a243` (v0.24.0-unstable021) asserting `plainHTTPHandler` answers **404** for non-HTTP-only hosts. The fork's `dd395ed2` (**PR #576**, open) restores the classic HTTP→HTTPS **301**, plus a local-IP/Constellation passthrough. Two intentional behaviours for the same function; the test encoded upstream's choice.
- **Resolution (2026-11-14): fork-owned divergence, decided deliberately.** #576 stays **open** upstream — closing it "in favour of" upstream's 404 would be a false record, since upstream never had the redirect to fix; commit 748302d ("[skip ci] Constellation work") removed it and nothing replaced it. The fork's vendored `src/zones_test.go` now asserts **301** for the two hostnames, each line commented with #576. `go test ./src/...` is **green**.
- **The `192.168.1.10` case was a broken fixture, not a behaviour difference.** The test never set `RemoteAddr`, so `httptest`'s default **`1.2.3.4`** applied — not loopback, not private, not CGNAT — making `IsLocalIP` false and the passthrough branch unreachable. In a real deployment `http://192.168.1.10` from a LAN client has a private `RemoteAddr` and **is served directly, no redirect**, in both the fork's and upstream's code. The fixture now sets `RemoteAddr` explicitly and covers both directions (remote → 301, local/CGNAT → served), so it tests the passthrough instead of accidentally bypassing it.
- **Assertion is load-bearing, verified by mutation:** replacing the fork's `http.Redirect(...StatusMovedPermanently)` line with upstream's `http.NotFound(w, r)` (passthrough untouched) fails exactly the three redirect cases. Reverting the whole handler fails all five. The test cannot pass by accident.
- **Why the redirect is the right call:** plain-HTTP mode is untouched — `plainHTTPHandler` gates on `HostIsHTTPOnly` (zone mode `DISABLED`) **first** and serves the router, so a zone configured for plain HTTP never sees a redirect. `IsLocalIP` covers loopback, private, CGNAT `100.64/10` and `192.0.0/24`, so localhost/LAN and Constellation peers are served directly (no redirect loop), and Cosmos issues certs via **DNS-01** — there is no ACME http-01 listener on the HTTP port, so the usual "redirect breaks cert issuance" objection does not apply.
- **Not verified / open:** whether upstream's 404 is a deliberate hostname-confirmation guard (a cacheable redirect reveals which hosts a server fronts) — argued, not established. If upstream says yes, #576 closes and this divergence reverts.
- **Known latent bug in the fork's own line, not fixed here:** `"https://"+r.Host+r.URL.String()` concatenates, so an **absolute-form** request target (`GET http://host/`, legal HTTP/1.1, sent by some proxies) yields `Location: https://hosthttp://host/`. Browsers send origin-form, so normal traffic is fine; the assertion scope above is status codes, which is unaffected. Clean fix is `r.URL.Scheme = "https"; r.URL.Host = r.Host; http.Redirect(w, r, r.URL.String(), code)`.

## Rebase checklist (this tree, all green)

```bash
export PATH=/usr/simplify/tools/go/bin:$PATH     # go 1.27.3, matches go.mod
cp src/update.go src/launcher/update.go          # gitignored; build.sh does this first
env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /tmp/b/cosmos src/*.go
env CGO_ENABLED=0 GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o /tmp/b/arm64 src/*.go
env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /tmp/b/launcher ./src/launcher/launcher.go ./src/launcher/update.go
env CGO_ENABLED=0 go test ./src/...              # all packages ok (needs the update.go copy above)
npm install && npm run client-build              # vite build, OK
cd go-sdk && go build ./...                      # shm_size hand-patch compiles
```

Results for this rebase: `cosmos` ✅ (137 MB), `cosmos-arm64` ✅, `cosmos-launcher` ✅, `npm run client-build` ✅ (`✓ built in 24.35s`), `go test ./src/...` **all packages ok** (the #576 divergence is now asserted, not tolerated).

**Anchors** (scope `src client go-sdk`; old fork tip = new rebased tree, verified equal):

| pattern | count | note |
|---|---|---|
| `depends_on` | 87 | compose-parity feature intact |
| `hjson` | 85 | HJSON editor + comment persistence |
| `shm_size` | 17 | 2 of them hand-patched in `go-sdk/client.gen.go` |
| `__cosmos_probe` | 3 | head-probe middleware (needs #580 COOP/COEP for cross-origin) |
| `robots_policy` | 2 | per-route indexing policy (fork-only, no upstream PR) |
| `5-field` | 12 | crontab support |
| `-m conntrack --ctstate` | 7 | iptables deprecation fix (master ledger F4; on unstable it is baseline) |
| `readOnly` | 37 fork-owned | 221 total − 184 upstream's own = 37, identical delta to old tip |

## Traps (found, NOT fixed — out of scope)

- **`build.yml` fires on `push` to `master`/`unstable` and publishes a release** (GHCR + GH Release `v${VERSION}` + zips, `ncipollo/release-action` with `allowUpdates: true`). A PR shows **0 check-runs** — CI only proves itself after merge. No `concurrency` group, so two pushes in a row race on the same release. This file carries `[skip ci]`-style protection **not** in the workflow: write `[skip ci]` in the commit message of docs-only pushes; GitHub honours it natively.
- **`build.yml` pins `node-version: 16`, `.nvmrc` says 25** (upstream's own CircleCI also uses v16). Client build here used node 22 and passed; pin mismatch is upstream-side debt.
- **`scripts/pre-commit`** (upstream's own, installed by `scripts/install-hooks.sh`): its `isPro` guard regex was fixed on this branch by `c15b1b2c` → `isPro\s*:\s*\(\)\s*=>\s*false`, which now matches upstream v0.24.0's stub. It also runs `scripts/generate-api.sh` on every commit → needs `go` on PATH and **regenerates `go-sdk/client.gen.go`, wiping the hand-patched `shm_size` fields** (api-docs never documented `shm_size`: `grep -c shm_size api-docs/openapi.json` = **0**). Proper fix: add the swagger annotation so codegen emits it.
- During a rebase, disable hooks with `git -c core.hooksPath=/tmp/nohooks …` **per command**. Never leave `core.hooksPath` set: it lives in the shared `.git/config` and silently disables the safety gate for the main clone too. (Verified unset after this rebase.)
- `git cherry-pick --no-verify` does not exist on this git; use the `hooksPath` trick.
- `locales/en/translation.json` is the one file whose +/- counts legitimately change: upstream itself deleted 8 keys (`mgmt.config.certRenewal*`, `mgmt.config.proxy.refreshNeededWarning.*`, `mgmt.shield.bans.empty`, `mgmt.shield.whitelist.help/.empty`) in unstable031. Verified **zero** code references remain — dropping them is correct, not a loss. Fork-only keys present: 54 (was 62 at old tip; the 8 delta = upstream's deletions).

## Refs

| ref | what |
|---|---|
| `upstream/unstable` = `5ee16623` | v0.24.0-unstable031 — new baseline |
| `backup/pre-rebase-unstable-20261113` = `0d1620ff` | old fork tip (pre-rebase, pre-rewrite) — **the backup** |
| `origin/unstable` | rewritten: upstream031 + 39 fork commits, one per feature/fix |
| merge-base `27f7f447` | v0.24.0-unstable030 |
| `backup/pre-rebase-unstable-20261022` = `0ac2630a` | older `unstable` snapshot |
| `backup/unstable` / `backup/unstable-as-is-3f1bfeac` = `3f1bfeac` | older snapshot |
| PR #51 | `rebase/master-v0.24.0` → `master` (the master-side rebase, ledger on `master`) |
| PR #52 | this one: `rebase/unstable-v0.31` → `unstable` |

*Update this file in the same PR/commit as every rebase — the ledger is only useful if it stays true.*
