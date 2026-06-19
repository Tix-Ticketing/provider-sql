# SingleStore flavour — development & packaging

This fork adds a `singlestore.sql.crossplane.io` API group (ProviderConfig, User,
Grant) on top of the upstream `crossplane-contrib/provider-sql`. SingleStore
speaks the MySQL wire protocol, so the MySQL client is reused; the separate API
group lets the SingleStore build run **in parallel** with the upstream provider.

## Local SingleStore for testing

A free single-node dev image is published by SingleStore. Run it locally:

```bash
docker run -d --platform linux/amd64 --name singlestoredb-dev \
  -e ROOT_PASSWORD="tester" \
  -p 3306:3306 -p 8080:8080 -p 9000:9000 \
  ghcr.io/singlestore-labs/singlestoredb-dev:latest
```

- `3306` — MySQL protocol (what provider-sql connects to)
- `8080` — Studio UI
- `9000` — Data API

Connect with any MySQL client, or via the bundled client:

```bash
docker exec singlestoredb-dev singlestore -uroot -ptester -e "SELECT @@version;"
# 5.7.32  SingleStoreDB source distribution (reports as MySQL 5.7 for compatibility)
```

### SQL forms this provider emits (verified against the dev image)

| Action | Statement |
|---|---|
| JWT user | `CREATE USER '<name>'@'%' IDENTIFIED WITH authentication_jwt REQUIRE SSL` |
| Password user | `CREATE USER '<name>'@'%' IDENTIFIED BY '<pw>' [REQUIRE SSL]` |
| Password change | `ALTER USER '<name>'@'%' IDENTIFIED BY '<pw>'` |
| Drop | `DROP USER IF EXISTS '<name>'@'%'` |
| Grant | `GRANT <privs> ON <db>.<table> TO '<name>'@'%' [WITH GRANT OPTION]` |
| Observe user | `SHOW GRANTS FOR '<name>'@'%'` (missing user → error `1141`) |

Gotchas confirmed live:
- `REQUIRE SSL` is **mandatory** for JWT users — `CREATE` fails without it. The
  controller always appends it for `authPlugin: jwt`.
- `SHOW GRANTS` on the `*.*` line carries a trailing auth clause, e.g.
  `... IDENTIFIED BY PASSWORD '*<hash>' REQUIRE SSL` or
  `... IDENTIFIED WITH authentication_jwt REQUIRE SSL`. The grant parser captures
  the tail and only looks for `WITH GRANT OPTION` in it.

## Flavour gate

`cmd/provider` accepts `--flavours` (env `FLAVOURS`), a comma list of
`mysql,postgresql,mssql,singlestore`. Default is all four (upstream behaviour).

Run only the SingleStore controllers:

```bash
FLAVOURS=singlestore provider-sql
```

An unknown flavour is a hard startup error (no silent no-op).

## Building the singlestore-only package

The full provider generates every group's CRD into `package/crds`. The
singlestore-only package ships **only** the four `singlestore.sql.crossplane.io`
CRDs so it can be installed next to upstream `provider-sql` with no CRD overlap.

```bash
make generate                        # refresh package/crds + zz_*
hack/stage-singlestore-pkg.sh        # copy singlestore CRDs -> package/singlestore/crds

# build the controller image (same binary for both packages)
docker build -f cluster/images/provider-sql/Dockerfile -t <registry>/provider-singlestore-runtime:<tag> .

# build + push the xpkg, embedding the runtime image
crank xpkg build \
  --package-root package/singlestore \
  --embed-runtime-image <registry>/provider-singlestore-runtime:<tag> \
  --package-file provider-singlestore.xpkg
crank xpkg push --package-files provider-singlestore.xpkg <registry>/provider-singlestore:<tag>
```

Install it with a `Provider` package CR and a `DeploymentRuntimeConfig` that sets
`FLAVOURS=singlestore` on the controller Deployment (see the infra repo:
`crossplane/shared/manifests/singlestore-access/`).

`package/singlestore/crds/` is generated and git-ignored; only
`package/singlestore/crossplane.yaml` is committed.
