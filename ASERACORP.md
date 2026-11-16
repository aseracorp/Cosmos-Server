# aseracorp fork — change ledger (`unstable`)

What this fork changes on top of upstream [`azukaar/Cosmos-Server`](https://github.com/azukaar/Cosmos-Server), feature by feature, so rebases are mechanical instead of archaeological. Most of it exists as an **upstream PR** — linked below, with its live status. `master` has its own ledger ([`ASERACORP.md` on `master`](https://github.com/aseracorp/Cosmos-Server/blob/master/ASERACORP.md)); this one tracks the `unstable` channel.

**Current baseline:** upstream `unstable` = `5ee16623` = **v0.24.0-unstable031**. Fork tip `origin/unstable` = `953b9572`: **41 commits / 135 files** (+12507 / −4046) on top of it, linear (0 merge commits), one commit per feature/fix. merge-base == the baseline itself, i.e. a true rebase, not a merge.

## PR status at a glance — two repos, two counts

**Upstream** [`azukaar/Cosmos-Server`](https://github.com/azukaar/Cosmos-Server/pulls): **51** PRs opened from this fork (`aseracorp` + `madejackson`): **7 merged**, **28 open**, **16 closed-unmerged**. Live `mergeable_state` against upstream `unstable`, re-read 2026-11-16: **26 clean, 2 dirty** (#581 `hostChip.jsx`, #582 `proxyman.jsx`). Every PR is accounted for in the [Full PR census](#full-pr-census) — read it before re-taking a PR, because **three open PRs are already shipped** (#495, #594, #627) and **one closed PR is already fixed upstream** (#592/#593).

**Fork repo** [`aseracorp/Cosmos-Server`](https://github.com/aseracorp/Cosmos-Server/pulls): **58** PRs — **42 merged**, **14 closed-unmerged**, **2 open**: [#58](https://github.com/aseracorp/Cosmos-Server/pull/58) (SOCKS5 relay routes) and [#36](https://github.com/aseracorp/Cosmos-Server/pull/36) (stale deSEC tokens). **Neither is in the tree — both are real gaps**, and #58 merges onto the current tip with 0 conflicts. The earlier version of this file counted only the upstream repo, so these two were invisible; see [Fork-repo PRs](#fork-repo-prs-aseracorpcosmos-server).

## How to use this during a rebase

```bash
git fetch upstream --prune
git checkout -b rebase/unstable-v0.31 upstream/unstable
git cherry-pick upstream/unstable..backup/pre-rebase-unstable-<date>   # the fork's own commits
```

Unlike `master`, `unstable` **can** be cherry-picked: the fork's commits sit directly on real upstream commits. The `master` "never cherry-pick, rebuild from net diff" rule exists only because upstream squash-force-pushed `master` (42 commits total, history rewritten) — see the master ledger.

History is **one commit per feature/fix** (43 raw fork commits → 41: the 3 `shm_size` commits and the 3-commit OIDC chain each squashed to one, the `hostChip.jsx` rebase fixup folded into the health-badges commit, and the `48c0143e` merge-damage repair split into probe-middleware + export-repair).

Then verify with the **net-diff equality check** (the real test, not "it compiles"):

```bash
MB=$(git merge-base <old-fork-tip> upstream/unstable)
git diff --numstat $MB <old-fork-tip>   | sort > /tmp/old.txt
git diff --numstat upstream/unstable HEAD | sort > /tmp/new.txt
diff /tmp/old.txt /tmp/new.txt   # only keys upstream itself deleted may differ
```

## The ledger — fork commits → upstream PRs

Verified by **patch-id** (`git show <fork commit> | git patch-id`) against every commit in each upstream PR head (`refs/pull/<N>/head`), plus file-set comparison and content greps where a change is folded into a bigger commit. `(clean)`/`(dirty)` = GitHub `mergeable_state` vs upstream `unstable`, 2026-11-16. `open` = PR still open upstream, i.e. the change is **not** in upstream.

| Fork commit | Change | Upstream PR | Status |
|---|---|---|---|
| `dce5647d` | `ci(release)`: restore fork build/publish workflow (`build.yml`) | — (fork infra; upstream-able parts [#444](https://github.com/azukaar/Cosmos-Server/pull/444), [#537](https://github.com/azukaar/Cosmos-Server/pull/537), both closed-unmerged) | fork infra |
| `5daf61f3` | `fix`: store admin email during stepped new-install setup (bad struct tag `json:"omitempty,email"`) | [#606](https://github.com/azukaar/Cosmos-Server/pull/606) (`clean`; earlier takes #604, #605 — closed dupes) | open |
| `1ac61ed9` | `fix(ui)`: container log timestamp extraction anchored + dedup | [#603](https://github.com/azukaar/Cosmos-Server/pull/603) (`clean`) | open |
| `86f01d5a` | `fix(ui)`: licence key never breaks at hyphens | [#602](https://github.com/azukaar/Cosmos-Server/pull/602) (`clean`) | open |
| `151bfe92` | `fix(ui)`: restore ServApp action button tooltips | [#601](https://github.com/azukaar/Cosmos-Server/pull/601) (`clean`) | open |
| `b419e076` | `fix(ui)`: decode URL-encoded segments in breadcrumbs | [#600](https://github.com/azukaar/Cosmos-Server/pull/600) (`clean`) | open |
| `8fc5e1ff` | `fix(ui)`: web terminal ghost key names (AltGraph, Process, Unidentified) | [#598](https://github.com/azukaar/Cosmos-Server/pull/598) (`clean`) | open |
| `f5241489` | `fix(ui)`: guard market detail render when app not found | [#597](https://github.com/azukaar/Cosmos-Server/pull/597) (`clean`) | open |
| `dd395ed2` | `fix(http)`: restore HTTP→HTTPS redirect handler ⚠️ **fork-owned divergence vs upstream's `zones_test.go` — decided, see Conflicts** | [#576](https://github.com/azukaar/Cosmos-Server/pull/576) (`clean`) | open |
| `61703829` | `fix(docker)`: healthcheck durations as compose-style strings | [#585](https://github.com/azukaar/Cosmos-Server/pull/585) (`clean`) — **defines `DurationStr`, consumed by #595** | open |
| `f1547c0b` | `fix(docker)`: accept string-form healthcheck `test:` in compose imports | [#595](https://github.com/azukaar/Cosmos-Server/pull/595) (`clean`) — **content dependency on #585**, see couplings | open |
| `c0059e5e` | `shm_size`: support + compose import + byte-size strings — **one commit** (was 3: `34d247d8`/`e2c8431b`/`db6171c5`) | [#567](https://github.com/azukaar/Cosmos-Server/pull/567) (`clean`) | open |
| `14fe453f` | `feat(market)`: lazily generate Passwords/Secrets from template refs | [#586](https://github.com/azukaar/Cosmos-Server/pull/586) (`clean`) | open |
| `1246cdc7` | `fix(mounts)`: normalize volume field casing + back-compat | [#573](https://github.com/azukaar/Cosmos-Server/pull/573) (`clean`) | open |
| `9a3c6c4d` | `feat(docker)`: end-to-end `depends_on` with compose parity | [#579](https://github.com/azukaar/Cosmos-Server/pull/579) (`clean`; earlier take #578, closed) | open |
| `9eac3a8d` | `feat(docker)`: volume mount `readOnly`, subpath, volume-nocopy | [#599](https://github.com/azukaar/Cosmos-Server/pull/599) (`clean`) — PR head carries extra files, so patch-id misses; content confirmed (18/18 fork hits on its translation keys) | open |
| `b2ba0df7` | `feat(ui)`: display/edit JSON payloads as HJSON | [#584](https://github.com/azukaar/Cosmos-Server/pull/584) (`clean`) | open |
| `901f55b6` | `feat(cron)`: standard 5-field crontabs + schedule validation | [#596](https://github.com/azukaar/Cosmos-Server/pull/596) (`clean`) | open |
| `60cc9093` | `feat(docker)`: user-set vs runtime config + persist HJSON comments. **Also carries the exec-form command/entrypoint work** (`NormalizeCmdArgs`, jsx passthrough) — see the #495 row | [#590](https://github.com/azukaar/Cosmos-Server/pull/590) (`clean`; earlier take #589, closed) + [#495](https://github.com/azukaar/Cosmos-Server/pull/495) (`clean`) | open |
| `dffff580` | `fix(servapps)`: HostChip status dot reads the real HTTP status. **Depends on fork-only middleware**: the PR head calls `utils.HeadProbeCORS` (`src/httpServer.go:949`) that its own diff never adds — see couplings. Adds the COOP/COEP/CORP strip in `src/proxy/routeTo.go` itself (9 lines) | [#582](https://github.com/azukaar/Cosmos-Server/pull/582) (`dirty` — conflicts on `proxyman.jsx`). [#580](https://github.com/azukaar/Cosmos-Server/pull/580) (closed) extends #582's strip; it is **not** what #582 needs | open / #580 closed |
| `52ffdf7d` | `fix(ui)`: valid input type for nickname (Bitwarden Android autofill) | [#575](https://github.com/azukaar/Cosmos-Server/pull/575) (`clean`; #574 = same fix on `master`, closed) | open |
| `9b9e8605` | `fix(ui)`: OIDC route name without `__route_` prefix | [#570](https://github.com/azukaar/Cosmos-Server/pull/570) (`clean`) | open |
| `c9c87efe` | build size: `-trimpath`, `-s -w`, `upx -9` | [#565](https://github.com/azukaar/Cosmos-Server/pull/565) (`clean`) | open |
| `3fc0ac81` | `feat(servapps)`: health-aware status badges (absorbs the `hostChip.jsx` rebase fixup `edf89b51`) | [#581](https://github.com/azukaar/Cosmos-Server/pull/581) (`dirty` — conflicts on `hostChip.jsx`). Builds standalone ✅; its head tree has no `HeadProbeCORS` | open |
| `685a7ede` | `fix(docker)`: start recreated container after update (unless stopped/lazy) | [#620](https://github.com/azukaar/Cosmos-Server/pull/620) (`clean`; re-taken as #617→#618→#619, all closed) | open |
| `45fe47b4` | `fix(oidc)`: host-agnostic discovery + issuer from the same host lookup — **one commit** (was the fork #41/#46/#48 chain: `1c0a90a2`/`efb9c318`/`0347ae00`) | [#627](https://github.com/azukaar/Cosmos-Server/pull/627) (`clean`) — the PR is the **consolidated** 2-file form; the fork commit spans 5 files (adds `oauth2_auth.go`, `oauth2_discover.go`, `oauth2_discover_test.go`) | open |
| `bffc0766` | `fix(docker)`: merge top-level `Mounts` so anonymous image volumes survive export | carried by the [#599](https://github.com/azukaar/Cosmos-Server/pull/599) mount work (no standalone PR) | open |
| `924f8eb3` | `fix(proxy)`: `headProbeWriter`/`HeadProbeCORS`/`originUnderDomain` middleware for the `__cosmos_probe` HEAD probe (half of old `48c0143e`) — **the only definition of `HeadProbeCORS` anywhere** (upstream031: 0; #582's head: 0). **Also carries #594**: `IsStreamingEndpoint` + the `MiddlewareTimeout` bypass + `middleware_test.go` (same 2 files as the PR) | fork-only middleware; [#594](https://github.com/azukaar/Cosmos-Server/pull/594) (`clean`, same 2 files) | #594 open |
| `487fe7c0` | `fix(build)`: repair compose-export/blueprint merge damage (other half of old `48c0143e`) | **none** — fork-only repair | — |
| `03849c55` | `fix(http)`: don't fatal on empty inapplicable TLS cert pairs at first setup | **none** — fork-only, needs a PR | — |
| `7bc9c2e6` | `fix(lazy)`: derive dormant from upstream semantics, drop persisted `cosmos-lazy-dormant` label (fork #30) | **none** — fork-only (touches `lazy.go`, which #581/#582 also edit) | — |
| `bd69b01c` | `fix(config)`: atomic config writes + serialised read/modify/write (fork #37) | **none** — fork-only, needs a PR | — |
| `c12adfec` | `feat(proxy)`: per-route search-engine indexing policy (fork #38) | **none** — fork-only, needs a PR | — |
| `e89f5389` `1eb6e645` | `fix(docker)`: servapp memory/CPU limits apply + stick; expose swap/soft limits (fork #49, #50) | **none** — fork-only, needs a PR (the merged #569 "zram/swap under disks" is a *different* feature) | — |
| `c15b1b2c` | `fix(build)`: correct Pro-leak guard regex in `scripts/pre-commit` | **none** — upstream's own hook, see **Traps** | — |
| `3e6e4093` | `chore(update)`: check updates on `aseracorp` (fork #44) | **none** — deliberate fork identity, keep out of upstream | — |
| `fe1f2d1d` | `chore`: regenerate `api-docs/` + Go SDK (follows #590/#599) | — (chore) | — |
| `61050b73` | `docs`: this ledger | — (fork docs) | — |
| `18935b2e` | `test(http)`: assert the 301 + real `RemoteAddr` fixture (fork PR #53) | **merged** into the fork's `unstable` | done |
| `953b9572` | `ci`: PR-time Go test workflow `test.yml` (fork PR #57) | **merged** into the fork's `unstable` | done |

Fork contributions **merged upstream**: [#569](https://github.com/azukaar/Cosmos-Server/pull/569) (fix zram/swap shown under disks) and the 2024 i18n series [#273](https://github.com/azukaar/Cosmos-Server/pull/273) [#303](https://github.com/azukaar/Cosmos-Server/pull/303) [#318](https://github.com/azukaar/Cosmos-Server/pull/318) [#324](https://github.com/azukaar/Cosmos-Server/pull/324) [#327](https://github.com/azukaar/Cosmos-Server/pull/327) [#329](https://github.com/azukaar/Cosmos-Server/pull/329).

## Full PR census

Every PR opened against **upstream** `azukaar/Cosmos-Server` (51 of them), classified against the tree at `origin/unstable` (tip `953b9572`). **Scope**: this grid is upstream-PR-only. The fork repo's own 58 PRs are in [Fork-repo PRs](#fork-repo-prs-aseracorpcosmos-server) — an earlier heading said "every PR this fork opened" over this grid alone, which was false: the two PRs open **in the fork repo** (#58, #36) are both unlanded and appeared nowhere. **This section was missing before**: the ledger above is keyed on *fork commits*, so any PR without a fork commit of its own disappears from the file. Several PRs — #495, #594, #615 (open) and #281, #444, #537, #607, #623 (closed-unmerged) — were previously named only in prose, if at all.

### Open PRs (28) — every one accounted for

| PR | Fix | GitHub | In the fork tree? |
|---|---|---|---|
| [#495](https://github.com/azukaar/Cosmos-Server/pull/495) `[FEAT] exec-form command/entrypoint` | `NormalizeCmdArgs` + exec/shell-form passthrough | `clean` | **yes — folded into `60cc9093`**: `NormalizeCmdArgs` 4 hits in `src/docker/`, jsx passthrough comments at `docker-compose.jsx:243-245`/`305-306`. No standalone fork commit → patch-id misses. **An open PR that is already shipped** |
| [#594](https://github.com/azukaar/Cosmos-Server/pull/594) `fix(api): 45s timeout kills streaming installs` | `IsStreamingEndpoint` + `MiddlewareTimeout` bypass | `clean` | **yes — folded into `924f8eb3`**, same 2 files (`src/utils/middleware.go`, `middleware_test.go`). The 45s timeout at `httpServer.go:760` is still there **and** bypassed for streaming routes. **An open PR that is already shipped** |
| [#615](https://github.com/azukaar/Cosmos-Server/pull/615) `fix(install): skip source-less volumes` | guard `volume.source` for tmpfs `{target, type}` mounts | `clean` | **NO — fork has this bug.** Crash site live at `client/src/pages/servapps/containers/docker-compose.jsx:832`: `!volume.source.startsWith('/')` with no `source` guard → `TypeError` on a source-less mount. Rebase onto unstable031 and take it |
| [#627](https://github.com/azukaar/Cosmos-Server/pull/627) `fix(oidc): host-agnostic discovery + issuer` | one host lookup for both | `clean` | yes — `45fe47b4` (fork form spans 5 files, the PR is the consolidated 2-file form; shared symbols confirmed in the tree) |
| [#620](https://github.com/azukaar/Cosmos-Server/pull/620) (+ closed #617/#618/#619) `fix(docker)`: start recreated container after update | unless stopped/lazy | `clean` | yes — `685a7ede`, file-set identical to the PR head |
| [#606](https://github.com/azukaar/Cosmos-Server/pull/606) (+ closed #604/#605) `fix`: admin email on stepped install | struct tag | `clean` | yes — `5daf61f3` (patch-id + file-set) |
| [#603](https://github.com/azukaar/Cosmos-Server/pull/603) `fix(ui)`: container log timestamps | anchor + dedup | `clean` | yes — `1ac61ed9` (patch-id + file-set) |
| [#602](https://github.com/azukaar/Cosmos-Server/pull/602) `fix(ui)`: licence key hyphen break | no line-break at hyphens | `clean` | yes — `86f01d5a` (patch-id + file-set) |
| [#601](https://github.com/azukaar/Cosmos-Server/pull/601) `fix(ui)`: ServApp action tooltips | update/pause/stop/… | `clean` | yes — `151bfe92` (patch-id + file-set) |
| [#600](https://github.com/azukaar/Cosmos-Server/pull/600) `fix(ui)`: breadcrumbs | decode URL-encoded segments | `clean` | yes — `b419e076` (patch-id + file-set) |
| [#599](https://github.com/azukaar/Cosmos-Server/pull/599) `feat(docker)`: mount readOnly/subpath/nocopy | + top-level `Mounts` merge (`bffc0766`) | `clean` | yes — `9eac3a8d` (+`bffc0766`). PR head has 2 files the fork commit lacks (`api_updateContainer.go`, `checkPorts.go`) → patch-id misses; content confirmed (18/18 fork hits on its translation keys) |
| [#598](https://github.com/azukaar/Cosmos-Server/pull/598) `fix(ui)`: web terminal ghost keys | AltGraph/Process/Unidentified | `clean` | yes — `8fc5e1ff` (patch-id + file-set) |
| [#597](https://github.com/azukaar/Cosmos-Server/pull/597) `fix(ui)`: market detail guard | app-not-found render | `clean` | yes — `f5241489` (patch-id + file-set) |
| [#596](https://github.com/azukaar/Cosmos-Server/pull/596) `feat(cron)`: 5-field crontabs | + schedule validation | `clean` | yes — `901f55b6` (patch-id + file-set) |
| [#595](https://github.com/azukaar/Cosmos-Server/pull/595) `fix(docker)`: string-form healthcheck `test:` | compose imports | `clean` ⚠️ | yes — `f1547c0b` (file-set identical). **`clean` but does not compile alone** (see couplings) |
| [#590](https://github.com/azukaar/Cosmos-Server/pull/590) (+ closed #589) `feat(docker)`: user-set vs runtime config | + HJSON comment persistence | `clean` | yes — `60cc9093` (14 files fork / 43 files PR). **Stacked PR**: its head diff also carries #585's `DurationStr` and #495's `NormalizeCmdArgs`, so patch-id misses. Head builds standalone ✅ |
| [#586](https://github.com/azukaar/Cosmos-Server/pull/586) `feat(market)`: lazily generated secrets | 64-hex from template refs | `clean` | yes — `14fe453f` (file-set identical) |
| [#585](https://github.com/azukaar/Cosmos-Server/pull/585) `fix(docker)`: healthcheck durations as strings | **defines `DurationStr`** | `clean` | yes — `61703829` (patch-id + file-set) |
| [#584](https://github.com/azukaar/Cosmos-Server/pull/584) `feat(ui)`: HJSON for JSON payloads | min-quote writer | `clean` | yes — `b2ba0df7` (file-set identical) |
| [#582](https://github.com/azukaar/Cosmos-Server/pull/582) `fix(servapps)`: HostChip reads real HTTP status | + COOP/COEP/CORP strip in `routeTo.go` | **`dirty`** (`proxyman.jsx`) | yes — `dffff580` (file-set identical). **Needs fork-only `HeadProbeCORS`** to compile — see couplings |
| [#581](https://github.com/azukaar/Cosmos-Server/pull/581) `feat(servapps)`: health-aware status badges | creates `client/src/utils/container-status.jsx` | **`dirty`** (`hostChip.jsx`) | yes — `3fc0ac81` (carries the `edf89b51` fixup → 30 files vs the PR's 32, patch-id misses). Builds standalone ✅ |
| [#579](https://github.com/azukaar/Cosmos-Server/pull/579) (+ closed #578) `feat(docker)`: end-to-end `depends_on` | compose parity, restart cascade | `clean` | yes — `9a3c6c4d` (patch-id + file-set) |
| [#576](https://github.com/azukaar/Cosmos-Server/pull/576) `fix(http)`: restore HTTP→HTTPS redirect | + local-IP/Constellation passthrough | `clean` | yes — `dd395ed2` (patch-id; fork commit touches 2 files, the PR 1 — the extra is `httpServer.go`); **fork-owned test divergence**, see Conflicts |
| [#575](https://github.com/azukaar/Cosmos-Server/pull/575) (+ closed #574 on `master`) `fix(ui)`: nickname input type | Bitwarden Android autofill | `clean` | yes — `52ffdf7d` (patch-id + file-set) |
| [#573](https://github.com/azukaar/Cosmos-Server/pull/573) `fix(mounts)`: normalize volume field casing | back-compat | `clean` | yes — `1246cdc7` (file-set identical) |
| [#570](https://github.com/azukaar/Cosmos-Server/pull/570) `fix(ui)`: OIDC route name | strip `__route_` | `clean` | yes — `9b9e8605` (patch-id + file-set) |
| [#567](https://github.com/azukaar/Cosmos-Server/pull/567) `feat(docker)`: `shm_size` | + byte-size strings | `clean` | yes — `c0059e5e` (file-set identical; squashed from 3 commits) |
| [#565](https://github.com/azukaar/Cosmos-Server/pull/565) `build`: reduce binary size | `-trimpath` `-s -w` `upx -9` | `clean` | yes — `c9c87efe` (patch-id + file-set) |

**Tally: 25 of 28 open PRs are in the tree; 3 are not** (#615, plus #607 and #623 from the closed set that must be re-opened). Three open PRs (#495, #594, #627) are shipped **without** a matching standalone fork commit — so an open PR number is not evidence that a change is missing, and a fork commit that cites a PR number is not evidence that it landed.

### Closed-unmerged PRs (16) — the re-open queue

| PR | Why closed | In the fork tree? |
|---|---|---|
| [#607](https://github.com/azukaar/Cosmos-Server/pull/607) `fix(newInstall): stop wiping /config on clean install` | closed by madejackson 2026-11-14; base **`master`** | **NO — both channels carry the admin-user-loss bug.** `os.RemoveAll` is live at `src/newInstall.go:91` on fork `unstable` **and** upstream `unstable` **and** fork `master`. Fork `unstable` touches `newInstall.go` only in `5daf61f3` (admin email), never the wipe path. The PR head has the fix + a 103-line regression test. **Re-open against `unstable`** |
| [#593](https://github.com/azukaar/Cosmos-Server/pull/593) `fix(docker)`: parse bind IP from colon-separated port string (re-take) | closed 2026-09-05 | **yes — upstream shipped it.** `pr593` is an ancestor of `upstream/unstable` (commit `2529f206` "[release] v0.24.0-unstable006"). The fix is baseline code on both sides: `if len(ports) > 2` at upstream `api_blueprint.go:580` / fork `:962`; the pre-fix `len(portStuff) > 2` form is **absent from both trees**. **Do NOT re-open — record as merged** |
| [#592](https://github.com/azukaar/Cosmos-Server/pull/592) same fix (original) | closed 2026-09-03, superseded by #593 | superseded; the landed form is #593 |
| [#623](https://github.com/azukaar/Cosmos-Server/pull/623) `fix(letsencrypt): clean stale deSEC `_acme-challenge` tokens` | closed 2026-10-19 | **NO** — the PR head's only file is `docs/DNS-challenge-diagnostics-and-fixes.md` (a write-up, not code; the title overstates it). **NO** — the PR head's only file is `docs/DNS-challenge-diagnostics-and-fixes.md` (a write-up, not code; the title overstates it), and that file is **absent from all four refs** (fork `unstable`/`master`, upstream `unstable`/`master`). ⚠️ An earlier version of this row said "fork has deSEC code at `src/dnsrecords/provider.go`" — **false**: `src/dnsrecords` is **upstream's** package (added upstream in `63e5a243` = v0.24.0-unstable021; tree `427d855a` **byte-identical** in upstream031 and the fork tip, and the fork's net diff touches `certificates.go` by **0** lines). **Re-scope as code, then re-open — and that re-scope already exists: fork PR [#36](https://github.com/aseracorp/Cosmos-Server/pull/36)** (commit `82ac2c34`, +519 lines in `src/utils/certificates.go`, adds `cleanupStaleChallengeTokens` + `desecAPI`; 0 hits for both in the fork tip) |
| [#580](https://github.com/azukaar/Cosmos-Server/pull/580) `fix(proxy)`: strip COOP/COEP/CORP **even with header hardening off** | closed 2026-08-29 (2 commits) | **partially** — the fork has #582's version of the strip (9 `Cross-Origin` lines in `routeTo.go`; upstream031: 0). #580's "always strip" extension is **not** in the fork, and it is **not** what #582 needs (see couplings). Head touches only `routeTo.go`; defines no `HeadProbeCORS` |
| [#537](https://github.com/azukaar/Cosmos-Server/pull/537) disable cgo (musl/alpine builds) | closed 2026-07-03 | **NO** — the fork builds with `CGO_ENABLED=0` locally; upstream's CI untouched. Upstream-able after re-scope |
| [#444](https://github.com/azukaar/Cosmos-Server/pull/444) GH Actions build/publish | closed 2026-08-28 | **NO** — stays fork-only `build.yml`; see Traps. File-set differs from the fork's `dce5647d` (fork has `build.sh`, PR has `docker.sh`) |
| [#281](https://github.com/azukaar/Cosmos-Server/pull/281) i18n: translate market entries | closed 2024-08-05 (2024 i18n series; the rest merged) | **NO** — no market-translation code in the tree. 2024-era; only resurrect with the i18n series |
| [#619](https://github.com/azukaar/Cosmos-Server/pull/619) start-after-update (3rd take) | closed 2026-10-12 → [#620](https://github.com/azukaar/Cosmos-Server/pull/620) | yes, via #620's commit |
| [#618](https://github.com/azukaar/Cosmos-Server/pull/618) start-after-update (2nd take) | closed 2026-10-12 → #619/#620 | yes, via #620's commit |
| [#617](https://github.com/azukaar/Cosmos-Server/pull/617) start-after-update (1st take) | closed 2026-10-12 → #618/#619/#620 | yes, via #620's commit |
| [#605](https://github.com/azukaar/Cosmos-Server/pull/605) admin email (2nd take) | closed 2026-09-11 → [#606](https://github.com/azukaar/Cosmos-Server/pull/606) | yes, via #606's commit |
| [#604](https://github.com/azukaar/Cosmos-Server/pull/604) admin email (1st take) | closed 2026-09-11 → #605/#606 | yes, via #606's commit |
| [#589](https://github.com/azukaar/Cosmos-Server/pull/589) user-set vs runtime config (original) | closed 2026-09-01 → [#590](https://github.com/azukaar/Cosmos-Server/pull/590) | yes, via #590's commit |
| [#578](https://github.com/azukaar/Cosmos-Server/pull/578) `depends_on` (original) | closed 2026-08-28 → [#579](https://github.com/azukaar/Cosmos-Server/pull/579) | yes, via #579's commit |
| [#574](https://github.com/azukaar/Cosmos-Server/pull/574) nickname input type on `master` | closed 2026-08-24 → [#575](https://github.com/azukaar/Cosmos-Server/pull/575) | yes, via #575's commit |

### Merged upstream (7)

#273 (base `master`), #303, #318, #324, #327, #329 (2024 i18n series, base `unstable`), #569 (zram/swap under disks). Historic — no fork commit needed on `unstable`; the code is upstream baseline.

### Fork-repo PRs (`aseracorp/Cosmos-Server`)

58 PRs in the fork repo: **42 merged**, **14 closed-unmerged**, **2 open**. Classed against `origin/unstable` tip `953b9572`, measured 2026-11-16 with `merge-tree`, `git grep` on the fix's own signatures, and `go build`/`go test` on a real merge of the head into the tip.

| PR | State | In the tree? | Evidence |
|---|---|---|---|
| [#58](https://github.com/aseracorp/Cosmos-Server/pull/58) `feat(proxy)`: SOCKS5 relay routes (`socks5://`, TCP-only, dials addresses, no outbound resolution) | **open**, base `unstable` | **NO — real gap, and it is ready** | `socks5` in `src`: fork tip **0**, upstream **0**. 10 commits over merge-base `61050b73`; +2555/−19 across 21 files (`src/proxy/Socks5.go`, `Socks5Codec.go`, `Socks5Policy.go`, 1173 lines of tests, `client/src/utils/socksRoute.js`). **`merge-tree origin/unstable × forkpr58` → 0 conflicts** (only shared file is `ASERACORP.md`, auto-merged, and #58's own ledger row survives the merge). Merging the head into the tip in a scratch worktree: `go build ./src` **OK**, `go vet ./src/proxy` **OK**, `go test ./src/proxy` (SOCKS) **ok**, `go test ./src/utils` (wire contract) **ok**. The branch base (`61050b73`) lags the tip by 2 commits, both docs/CI — nothing to rebase for correctness, GitHub already says mergeable |
| [#36](https://github.com/aseracorp/Cosmos-Server/pull/36) `fix(letsencrypt)`: clean stale deSEC `_acme-challenge` tokens | **open**, base `unstable` | **NO — real gap** | The fix commit is `82ac2c34` (+519 lines, `src/utils/certificates.go` only). Signatures `cleanupStaleChallengeTokens` and `desecAPI`: fork tip **0**, upstream **0** (other hits on that commit's function list — `generateCA`, `DoLetsEncrypt`, `GetEmail` — are **name collisions** with code that predates the PR; only the PR-specific names prove landing). The PR **head** is a stale branch: `merge-tree origin/unstable × forkpr36` → **7 conflicts** (`build.yml` add/add, `hostChip.jsx`, `proxyman.jsx`, `newService.jsx`, `setup.jsx`, `en/translation.json`, `go-sdk/client.gen.go`). Take `82ac2c34` alone, not the head |
| [#31](https://github.com/aseracorp/Cosmos-Server/pull/31) `fix(client)`: dormant lazy apps on home + health only while running | closed-unmerged 2026-09-22 | **partially — one piece is missing** | The dormant half **is** in the tree (`isContainerDormant` 2 hits at tip, 0 upstream, via `7bc9c2e6`/fork #30). The **`completed` vs `exited` split is not**: `container-status.jsx` at the PR head `53edd492` has 14 `completed` hits and derives the state from `State.ExitCode`; the tip file has **0**, and 7 in `client` at the tip = upstream baseline. Not an ancestor of the tip, and `3fc0ac81` (#581) is **not** an ancestor of it, so the tip's richer file does not subsume this hunk. Re-take just that hunk |
| [#34](https://github.com/aseracorp/Cosmos-Server/pull/34) deSEC auto-setup, [#33](https://github.com/aseracorp/Cosmos-Server/pull/33)/[#21](https://github.com/aseracorp/Cosmos-Server/pull/21)/[#16](https://github.com/aseracorp/Cosmos-Server/pull/16)/[#20](https://github.com/aseracorp/Cosmos-Server/pull/20) multicast/broadcast relay | closed-unmerged | **NO — deliberately, keep it that way** | `oneuptime` in `src`: **0** both sides; `desec` in `src/newInstall.go`: **0**; `broadcast`/`multicast`/`Broadcast` in `src/docker`: **0** both sides. The deSEC exclusion is an explicit standing instruction from the 2026-10-09 cycle (feature removed from `unstable`, kept only on an unmerged PR) — record as **decision**, not gap |
| [#52](https://github.com/aseracorp/Cosmos-Server/pull/52) rebase `unstable` onto unstable031 + ledger | closed-unmerged | **yes, superseded** | Head `61050b73` **is an ancestor of the tip** — the branch landed by other means (the rewrite was pushed to `unstable` directly), so closed-unmerged here means "landed, not via this PR" |
| [#54](https://github.com/aseracorp/Cosmos-Server/pull/54)/[#55](https://github.com/aseracorp/Cosmos-Server/pull/55)/[#56](https://github.com/aseracorp/Cosmos-Server/pull/56) CI probes, [#4](https://github.com/aseracorp/Cosmos-Server/pull/4)/[#8](https://github.com/aseracorp/Cosmos-Server/pull/8)/[#9](https://github.com/aseracorp/Cosmos-Server/pull/9) "test" | closed-unmerged | n/a | Throwaway PRs opened to learn GitHub's `on: pull_request` workflow discovery rules (the `test.yml` split, see Traps) and as cherry-pick sandboxes. No content to take |

**Fork-repo PRs whose content is in the tree**: the 42 merged ones are the fork's own change history (each landed on `unstable`/`master` as its own commit, which is why the fork's history is one-commit-per-feature); they map to the upstream PRs in the ledger grid above — e.g. fork #37 → atomic config writes (`bd69b01c`), fork #38 → `robots_policy` (`c12adfec`), fork #48/#46/#41 → upstream #627 (`45fe47b4`), fork #49/#50 → resource limits (`e89f5389`/`1eb6e645`).

## PR couplings (measured, not assumed)

⚠️ **The previous version of this section was wrong, and the reasoning behind it is recorded here so it is not re-invented.** It said *"582+581 (competing siblings), 595→585 (ordering)"* and claimed #582 "calls `utils.HeadProbeCORS`, which upstream does not define — it needs #580". The user correctly pushed back: **all of these PRs are on the fork's `unstable` and work side by side, and none of them describes its relation to the others in its PR description.** Nothing in this section may rest on PR prose. Everything below comes from cherry-pick, `merge-tree`, and `go build` runs on **clean worktrees checked out at the exact PR head** (`refs/pull/<N>/head`), re-run 2026-11-16.

| Relationship | Evidence | Verdict |
|---|---|---|
| **#595 needs #585's code — no ordering constraint** | `pr595` cherry-picked alone onto `upstream/unstable` merges **clean** (GitHub says `clean`) and then `go build ./src` fails: `src/docker/api_blueprint.go:56:15: undefined: DurationStr`. `DurationStr` is defined by #585. Picking `#585` then `#595` → build **OK**; picking `#595` then `#585` → build **OK** as well. | A real **content** dependency on the other PR's code, **not** a merge-order requirement. Land them in the same batch; the sequence is free. This is the proof that a `clean` badge is never a build verdict. |
| **#581 and #582 are not siblings/competitors — they are two features that share a file** | Both **add** `client/src/utils/container-status.jsx` (136 lines in #581, 101 in #582). `git merge-tree --write-tree pr581 pr582` → `CONFLICT (add/add)` on that file **and** `CONFLICT (content)` in `src/proxy/lazy.go`. Each also conflicts **on its own** against upstream031 — #581 on `client/src/components/hostChip.jsx`, #582 on `client/src/pages/config/users/proxyman.jsx` — so both are `dirty`, and **neither conflict involves the other PR**. In the fork they coexist on one **linear** branch (41 commits, 0 merges): `dffff580` (#582) lands earlier, `3fc0ac81` (#581) lands later and **overwrites** the shared file, so the shipped UI is #581's richer version. `3fc0ac81` is **not** an ancestor of `dffff580` — the order is an artifact of the rewrite, not of ancestry. | **Not** competing implementations of one feature. The overlap is a **merge** concern only if upstream ever takes both (then `container-status.jsx` needs a manual union) — it is **not** a fork defect. Nothing to fix here. |
| **#582 needs fork-only middleware, and #580 is NOT it** | A clean worktree at the `pr582` head (`git status` clean) has **no** `HeadProbeCORS` definition: the diff `pr582^..pr582` never touches `middleware.go`; `src/httpServer.go:949` calls `router.Use(utils.HeadProbeCORS)` (1 use, 0 definitions). Building that tree: **FAIL** — `src/httpServer.go:949:19: undefined: utils.HeadProbeCORS`. `func HeadProbeCORS` definitions: upstream031 **0**, pr582 **0**, fork tip **1**. #580 (closed) touches **only** `src/proxy/routeTo.go` and adds **no** `HeadProbeCORS` — it extends the COOP/COEP/CORP strip so it also runs when header hardening is off. #582 carries its **own** version of that strip (9 added lines in `routeTo.go`), and upstream031 has no strip at all (0 hits). | The old sentence was half right, half false. **True:** #582 references a symbol that does not exist upstream, so it cannot compile on an upstream tree — the missing piece is **fork-only `924f8eb3`**. **False:** that #580 is the provider, and that #582's patch is self-contained. #580 is an optional hardening extension, not a prerequisite. |
| **#581 is self-contained** | Clean worktree at the `pr581` head builds: **OK**. No `HeadProbeCORS` reference (0 defs, 0 uses). | #581 can land upstream on its own (after its `hostChip.jsx` conflict is resolved). |
| **#590 is a stacked PR** | Clean worktree at the `pr590` head builds: **OK** standalone, because its head diff carries the code it needs from #585 (`DurationStr`) and #495 (`NormalizeCmdArgs`). File-set: 43 files (PR) vs 14 (fork commit `60cc9093`). | Self-contained as a PR; on the fork its content is folded into `60cc9093` alongside #495's. Patch-id will miss — grep for the signatures. |
| **#594 is in the tree without its own commit** | `IsStreamingEndpoint` + the `MiddlewareTimeout` bypass + `middleware_test.go` land inside `924f8eb3` — the **same 2 files** as #594's head diff. The 45s timeout at `httpServer.go:760` is intact and bypassed for streaming routes. | Shipped; PR still open. Do not "re-take" #594 at a rebase — grep for `IsStreamingEndpoint` instead. |
| **#495 is in the tree without its own commit** | `NormalizeCmdArgs` (4 hits, `src/docker/api_blueprint.go`) + the jsx exec/shell-form passthrough comments inside `60cc9093` at `docker-compose.jsx:243-245` and `305-306`. | Shipped; PR still open. |
| **#615 and #607 are genuinely absent** | Content greps: `volume.source.startsWith` unguarded at `docker-compose.jsx:832` (fork **and** fork `master`:745); `os.RemoveAll` at `newInstall.go:91` on fork `unstable`, fork `master`, and upstream `unstable`. | Real gaps — **the fork carries the bugs these PRs fix.** Take #615; re-open #607 against `unstable`. |
| **#592/#593 are NOT a gap — upstream shipped them** | `pr593` is an ancestor of `upstream/unstable` (`2529f206` = v0.24.0-unstable006). Fixed form `if len(ports) > 2` present in upstream (`api_blueprint.go:580`) **and** fork (`:962`); pre-fix `len(portStuff) > 2` absent from both. | Closed-unmerged ≠ absent. **Do not re-open.** Re-check closed PRs for ancestry before adding them to the re-open queue. |

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
- **Resolution (2026-11-14): fork-owned divergence, decided deliberately.** #576 stays **open** upstream — closing it "in favour of" upstream's 404 would be a false record, since upstream never had the redirect to fix; commit `748302d` ("[skip ci] Constellation work") removed it and nothing replaced it. The fork's vendored `src/zones_test.go` now asserts **301** for the hostnames, each case commented with #576. `go test ./src/...` is **green**.
- **The `192.168.1.10` case was a broken fixture, not a behaviour difference.** The test never set `RemoteAddr`, so `httptest`'s default **`1.2.3.4`** applied — not loopback, not private, not CGNAT — making `IsLocalIP` false and the passthrough branch unreachable. In a real deployment `http://192.168.1.10` from a LAN client has a private `RemoteAddr` and **is served directly, no redirect**, in both the fork's and upstream's code. The fixture now sets `RemoteAddr` explicitly and covers both directions (remote → 301; local/CGNAT → served), so it tests the passthrough instead of accidentally bypassing it.
- **Assertion is load-bearing, verified by mutation:** replacing the fork's `http.Redirect(...StatusMovedPermanently)` line with upstream's `http.NotFound(w, r)` (passthrough untouched) fails exactly the redirect cases. Reverting the whole handler fails all of them. The test cannot pass by accident.
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
npm install && npm run client-build              # vite build
cd go-sdk && go build ./...                      # shm_size hand-patch compiles
```

Results for the current tip `953b9572`, re-run 2026-11-16: `cosmos` ✅ (~137 MB), `cosmos-arm64` ✅, `cosmos-launcher` ✅, `go test ./src/...` **all packages ok** (the #576 divergence is asserted, not tolerated).

**PR sweep is part of the rebase, not an afterthought.** Re-run the census (fetch every `refs/pull/<N>/head`, patch-id it against the tree, then grep the fix's signature code for every patch-id miss, **and check `git merge-base --is-ancestor pr<N> upstream/unstable` for closed PRs**), then refresh `mergeable_state` from the API — all of those move as upstream moves.

**Anchors** — recomputed on `origin/unstable` (tip `953b9572`) vs `upstream/unstable` (`5ee16623`). Method: `grep -ro -- "<pattern>" src client go-sdk | wc -l` (case-**sensitive**), run identically in both trees. The **delta** column (fork − base) is the number that must stay stable across rebases; the absolute counts move with upstream.

| pattern | fork | upstream031 | delta | note |
|---|---|---|---|---|
| `depends_on` | 89 | 16 | **73** | compose-parity feature intact |
| `hjson` | 34 | 0 | **34** | 98 case-insensitively (`HJSON` in prose/comments) |
| `shm_size` | 19 | 0 | **19** | 2 of them hand-patched in `go-sdk/client.gen.go` — see Traps |
| `__cosmos_probe` | 3 | 2 | **1** | upstream031 already ships `ProbeParam` + the `hostChip.jsx` probe call; the fork delta is the proxy answer path |
| `HeadProbeCORS` | 3 | 0 | **3** | fork-only middleware (`924f8eb3`); #582 calls it without defining it — see couplings |
| `robots_policy` | 2 | 0 | **2** | per-route indexing policy (fork-only, no upstream PR) |
| `5-field` | 12 | 0 | **12** | crontab support |
| `conntrack` | 5 | 5 | **0** | iptables deprecation fix (master ledger F4); on unstable it is upstream baseline |
| `readOnly` | 87 | 53 | **34** | fork-owned mount work; 252 case-insensitively |

⚠️ **An earlier version of this table was not reproducible** (87/85/37 with no stated method; none of those numbers come out of the tree under case-sensitive, case-insensitive, `git grep`, or delta counting). It was replaced by recomputed values, not adjusted. Recompute with a different scope and you get different absolutes — the deltas are the stable part.

**Translation keys** (`client/src/utils/locales/en/translation.json`, flattened to dotted leaf paths): fork **2326** keys, upstream031 **2272**, **fork-only 54**, **upstream-only 0** (nothing upstream is missing). Eight keys present at the pre-rebase tip were dropped because upstream itself deleted them (`mgmt.config.certRenewal{Title,Text,Linktext}`, `mgmt.config.proxy.refreshNeededWarning.{selfSigned,notDomain}`, `mgmt.shield.bans.empty`, `mgmt.shield.whitelist.{empty,help}`). Verified **zero** full-key references remain in `src`+`client` — dropping them is correct, not a loss. Leaf-word greps for these (`empty`, `selfSigned`) hit hundreds of times on unrelated text, so **match the full key path**, never the leaf.

## Traps (found, NOT fixed — out of scope)

- **`build.yml` fires on `push` to `master`/`unstable` and publishes a release** (GHCR + GH Release `v${VERSION}` + zips, `ncipollo/release-action` with `allowUpdates: true`). No `concurrency` group, so two pushes in a row race on the same release. PR-time testing now lives in a **separate** `test.yml` (fork PR #57, merged) — `pull_request` into `master`/`unstable`, `permissions: contents: read`, one run per PR branch — deliberately split so a PR can never reach the publish job. A PR into **upstream** `azukaar/Cosmos-Server` still gets **0 check-runs**: upstream has no test workflow, so upstream-side CI only proves itself after merge. Write `[skip ci]` in the commit message of docs-only pushes to `master`/`unstable`; GitHub honours it natively on the push event.
- **`build.yml` pins `node-version: 16`, `.nvmrc` says 25** (upstream's own CircleCI also uses v16). Client build here used node 22 and passed; pin mismatch is upstream-side debt.
- **`scripts/pre-commit`** (upstream's own, installed by `scripts/install-hooks.sh`): its `isPro` guard regex was fixed on this branch by `c15b1b2c` → `isPro\s*:\s*\(\)\s*=>\s*false`, which now matches upstream v0.24.0's stub. It also runs `scripts/generate-api.sh` on every commit → needs `go` on PATH and **regenerates `go-sdk/client.gen.go`, wiping the hand-patched `shm_size` fields** (api-docs never documented `shm_size`: `grep -c shm_size api-docs/openapi.json` = **0**). Proper fix: add the swagger annotation so codegen emits it.
- During a rebase, disable hooks with `git -c core.hooksPath=/tmp/nohooks …` **per command**. Never leave `core.hooksPath` set: it lives in the shared `.git/config` and silently disables the safety gate for the main clone too. (Verified unset after this rebase.)
- `git cherry-pick --no-verify` does not exist on this git; use the `hooksPath` trick.
- **Cherry-picking two PRs in one command leaves the sequencer running on conflict** — a later `git reset --hard` in the same loop then reports `cherry-pick is already in progress` and the whole measurement is garbage. `git cherry-pick --abort` before every reset, and count `CONFLICT` lines, not exit codes (a sequenced pick returns 128 for the aborted run, not the real conflict count).
- **A PR can be GitHub-`clean` and still not compile.** #595 merges onto upstream031 with no conflict and fails `go build` (`undefined: DurationStr`). `mergeable_state` is a text-merge verdict, never a build verdict — run the build on the merged result.
- **A patch-id miss does not mean "not in the tree", and a patch-id hit does not mean the PR is in the tree.** Folded commits (#495, #594) miss on patch-id; #273/#281/#303/#318/#324/#327/#329/#578 sweep up hundreds of historical commits (`git rev-list --count` of 635–807 over the baseline = the merge-base is ancient, so the comparison is meaningless). Scope the comparison and fall back to content greps.
- **Never conclude a dependency from "the symbol is missing in the base tree."** Check the PR head tree first (`git grep <symbol> pr<N> -- src/`). That is exactly how the false "#582 needs fork-only #580" claim was produced: `HeadProbeCORS` is absent upstream and **absent from #582's own patch** — the provider is fork-only `924f8eb3`.
- **Never conclude a PR is missing from "GitHub says closed-unmerged".** Check ancestry: `git merge-base --is-ancestor pr<N> upstream/unstable`. #593 (and its predecessor #592) closed unmerged and **landed in upstream's own release train** — it is baseline code, not a gap.
- **Scope a census to a repo, not to "the fork".** PRs live in **two** repos: upstream `azukaar/Cosmos-Server` (51) and the fork `aseracorp/Cosmos-Server` (58). Counting only upstream produced a section headed "every PR this fork opened" that silently omitted **both PRs open in the fork repo** — #58 (SOCKS5, 0 `socks5` hits in `src`) and #36 (stale deSEC tokens, 0 `cleanupStaleChallengeTokens`/`desecAPI` hits) — i.e. the census missed the two largest unlanded features it was supposed to be guarding. A heading that says "every X" must be backed by a query that enumerates X; here that is `/repos/<owner>/<repo>/pulls?state=all` **per repo**.
- **Function-name signature greps need PR-specific names.** `git show <commit> | grep '^+func '` on #36 lists `generateCA`, `DoLetsEncrypt`, `GetEmail` — all present in the tip at counts 2–4, because the PR **rewrote** them. Only the PR-invented names (`cleanupStaleChallengeTokens`, `desecAPI`) are admissible as evidence, and both are **0**. A rewritten function is not a signature.
- **`git worktree add` takes `<path> [<branch>]`** — reversing the arguments fails with `fatal: invalid reference: <path>`. Pair it with a `cd` guard, because a failed `add` leaves the shell in the **main clone**: that is exactly what happened here, and the follow-up `git rev-parse HEAD` printed the main clone's tip (`c40699a3`) instead of the branch tip. Check the `add` exit code **and** the printed HEAD against the expected ref.
- **`go build` in a fresh worktree exceeds a 60s synchronous shell window** — the module cache is cold and it spends the whole budget on `go: downloading`. A timeout there reads like a build failure and is not (`go vet` on the same tree passed). Warm the cache with `go mod download` in a background task, then build.
- **`git worktree` entries under `/tmp` die with `/tmp`** (cleared on reboot) — the commits survive in the object DB, so `git worktree prune` then re-`add` recovers the branch; no data loss.
- **`git worktree add` / `git merge-tree` need the repo as CWD.** A script that `cd`s into a worktree path that failed to create silently continues outside the repo, and every `go build` then reports `go.mod file not found` — which reads like a toolchain failure and is not. Check the worktree add's exit status, and assert `git rev-parse --short HEAD` matches the expected ref before measuring.

## Refs

| ref | what |
|---|---|
| `upstream/unstable` = `5ee16623` | v0.24.0-unstable031 — current baseline |
| `backup/pre-rebase-unstable-20261113` = `0d1620ff` | old fork tip (pre-rebase, pre-rewrite) — **the backup** |
| `origin/unstable` | rewritten: upstream031 + 41 fork commits, one per feature/fix (tip `953b9572`) |
| merge-base = `5ee16623` | the baseline is the merge-base (true rebase); previous cycle's merge-base was `27f7f447` = v0.24.0-unstable030 |
| `backup/pre-rebase-unstable-20261022` = `0ac2630a` | older `unstable` snapshot |
| `backup/unstable` / `backup/unstable-as-is-3f1bfeac` = `3f1bfeac` | older snapshot |
| PR #51 | `rebase/master-v0.24.0` → `master` (the master-side rebase, ledger on `master`) |
| PR #52 | the unstable rebase (closed unmerged — the rewritten branch was landed on `unstable` directly) |
| PR #53 | `test(http)` 301 assertion + `RemoteAddr` fixture — **merged** |
| PR #57 | `test.yml` PR-time Go test workflow — **merged** |
| `forkpr58` = `193c17ce` | fetched `refs/pull/58/head` of the **fork** repo (SOCKS5 relay, open, 0-conflict merge onto the tip, builds + tests green) |
| `forkpr36` = `b94c1aa4` | fetched `refs/pull/36/head` of the **fork** repo (stale deSEC tokens, open; take `82ac2c34` alone, head = 7 conflicts) |

*Update this file in the same PR/commit as every rebase — the ledger is only useful if it stays true.*
