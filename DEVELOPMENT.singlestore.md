# SingleStore flavour — development & packaging

This fork adds a `singlestore.sql.crossplane.io` API group (ProviderConfig, User,
Grant, Role, RBACGroup, Table) on top of the upstream
`crossplane-contrib/provider-sql`. SingleStore speaks the MySQL wire protocol, so
the MySQL client is reused; the separate API group lets the SingleStore build run
**in parallel** with the upstream provider.

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
singlestore-only package ships **only** the seven `singlestore.sql.crossplane.io`
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

## Table controller: SQL forms and lifecycle

The `Table` kind treats `createStatement` as opaque DDL: the controller runs it
verbatim and never parses or diffs the live schema. Drift is detected solely via
`schemaGeneration`, which the controller tracks in a per-database marker table
(`cdc_table_state`) alongside the data table itself.

| Action | Statement |
|---|---|
| Existence check (Observe) | `SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?` |
| Marker read (Observe) | `SELECT schema_generation FROM <db>.cdc_table_state WHERE target_name = ?` |
| Ensure marker table (Create/adopt/recreate) | `CREATE ROWSTORE REFERENCE TABLE IF NOT EXISTS <db>.cdc_table_state (target_name VARCHAR(255) NOT NULL PRIMARY KEY, schema_generation VARCHAR(80) NOT NULL, ready_at DATETIME NOT NULL)` |
| Marker upsert | `REPLACE INTO <db>.cdc_table_state VALUES ('<target_name>', '<schemaGeneration>', NOW())` |
| Create | runs the MR's `createStatement` verbatim |
| Recreate (Update, stale marker) | `DROP TABLE IF EXISTS <db>.<table>`, then re-run `createStatement`, then ensure-marker + upsert |
| Adopt (Update, no marker) | ensure-marker + upsert only - no DROP/CREATE of the data table |
| Delete | `DROP TABLE IF EXISTS <db>.<table>`, then `DELETE FROM <db>.cdc_table_state WHERE target_name = '<name>'` |

Implementation notes:
- The marker read tolerates server error `1146` (unknown table) as "no marker
  yet" - the `cdc_table_state` marker table may simply not have been created in
  that database yet.
- `createStatement` is produced upstream by SP2 (the generator), which already
  emits the correct storage keyword for the target table. The controller accepts
  all three forms unmodified: plain columnstore (`CREATE TABLE ...`, no
  keyword), rowstore (`CREATE ROWSTORE TABLE ...`), and reference
  (`CREATE ROWSTORE REFERENCE TABLE ...`).
- Recreate only fires when the marker's `schema_generation` differs from the
  MR's `schemaGeneration`; adopt (no marker row at all) never drops or recreates
  the data table, it only writes the marker so future generations can be
  compared.
- The `cdc_table_state` marker table is owned by the controller; do not drop it
  manually. Because a missing marker reads the same as "no marker row", dropping
  it while a `schemaGeneration` change is pending makes the next Update adopt
  (write the new marker, no DDL) instead of recreating, silently masking the
  drift until the generation changes again. Data is never lost, but the table
  keeps its old schema.
- Delete tolerates two server error codes: `1146` (unknown table - the marker
  was never created, e.g. the table was never adopted) and `1049` (unknown
  database - the parent database was already dropped out from under the
  table).
