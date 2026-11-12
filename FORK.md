# Tixly fork of provider-sql

This fork carries the **SingleStore** flavour (`singlestore.sql.crossplane.io`), shipped
as the `provider-singlestore` Crossplane package. It runs on the hub next to the
upstream `provider-sql` (`FLAVOURS=singlestore`).

## Branches

| Branch   | What it is | Who changes it |
|----------|------------|----------------|
| `master` | Exact mirror of upstream `crossplane-contrib/provider-sql`. Never commit here. | GitHub "Sync fork" only |
| `tixly`  | `master` + our commits on top. Default branch; builds come from here. | PRs, and the rebase below |

`git diff master tixly` is always exactly what we carry over upstream.

Rulesets: `master` can't be deleted or force-pushed (fast-forward only); `tixly`
can't be deleted (force-push is allowed for the rebase).

GitHub Actions is disabled on this fork. Upstream's workflows target their own
registry and secrets. Builds run in the hub's Argo Workflows (below), and tests run
locally before pushing.

## Sync with upstream

On each upstream release, or before bumping `provider-singlestore`:

```bash
# 1. Update master from upstream (always a fast-forward, never a conflict)
gh repo sync Tix-Ticketing/provider-sql -b master    # or the "Sync fork" button

# 2. Replay our commits on top of it
git fetch origin
git switch tixly && git pull --ff-only
git config rerere.enabled true                         # remember conflict resolutions
git rebase origin/master                               # conflicts: fix, git add, git rebase --continue

# 3. Verify before pushing
go build ./... && go vet ./... && go test ./...
(cd generate && go generate -tags generate ./...)      # only if API types changed; commit the result
git status                                              # should be clean

# 4. Publish
git push --force-with-lease origin tixly
```

Keep our work small and in our own paths (`apis/cluster/singlestore`,
`pkg/controller/cluster/singlestore`, `package/singlestore`, `hack/*singlestore*`) so
rebases rarely conflict. Shared upstream files we touch today: `cmd/provider/main.go`,
`pkg/controller/sql.go`, `apis/sql.go`, `pkg/clients/mysql/mysql.go`, `.gitignore`.

### After each rebase: keep the SingleStore flavour aligned with upstream

The SingleStore code is modelled on upstream's MySQL flavour. Check what upstream changed
and mirror it, in a separate commit on `tixly`:

```bash
git log --oneline <old-master>..origin/master -- \
  pkg/controller/cluster/mysql apis/cluster/mysql pkg/clients cmd go.mod
```

- **Shared types and APIs:** e.g. v0.16 moved `crossplane-runtime/apis/common/v1` to
  `crossplane/apis/v2/core/v2` and `register.go` to `runtime.NewSchemeBuilder`.
  Follow it, then `go generate`.
- **Controller `Setup`:** management policies, state metrics, reference resolvers
  and options should match `pkg/controller/cluster/mysql/*/reconciler.go`.
- **API markers:** printcolumns and CEL validations on `apis/cluster/mysql` have SingleStore
  equivalents (e.g. plugin/password mutual exclusivity).
- **Docker base image:** copy the distroless digest from `cluster/images/provider-sql/Dockerfile`
  into `cluster/images/provider-singlestore/Dockerfile`. Upstream's dependabot only
  bumps theirs.
- **CRD changes:** if a new CEL rule or required field is added, check that live objects on the hub
  still pass (`kubectl get users.singlestore.sql.crossplane.io -o json`) before rolling out.

## Build and release

1. A push to `tixly` fires the GitHub webhook to the hub's Argo Events
   (`provider-sql-push` EventSource, sensor `tixly-provider-singlestore-shared-euw1-sensor-build`
   in Tix-Ticketing/infrastructure `crossplane/shared/manifests/argo-events/config/`).
2. The `provider-xpkg-build` WorkflowTemplate builds and pushes
   `352695479602.dkr.ecr.eu-west-1.amazonaws.com/provider-singlestore:<7-char sha>`.
3. Nothing rolls out by itself. Roll out by bumping the package in infrastructure
   `crossplane/shared/manifests/crossplane/providers/provider-singlestore.yaml` via PR.
   It manages **prod** SingleStore access, so that bump goes through a change request.
4. Tag the commit you released, e.g. `git tag -a singlestore-v0.5.0 <sha>` and push the tag.
   Force-pushes to `tixly` would otherwise leave released commits unreferenced.

## History

Before 2026-11 our commits were on `master`. That history is kept at tags
`singlestore-v0.4.0` (deployed `provider-singlestore:v0.4.0`) and
`backup/master-before-patch-branch`. On `tixly` it was squashed into three commits.
