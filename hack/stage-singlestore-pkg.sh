#!/usr/bin/env bash
# Stage the singlestore-only Crossplane package root for `crank xpkg build`.
#
# The full provider generates every group's CRD into package/crds. The
# singlestore-only package must ship ONLY the singlestore.sql.crossplane.io CRDs
# so it can be installed alongside the upstream provider-sql without CRD overlap.
# This copies just those CRDs next to package/singlestore/crossplane.yaml.
#
# Usage:
#   hack/stage-singlestore-pkg.sh
#   crank xpkg build --package-root package/singlestore \
#     --embed-runtime-image <controller-image> --package-file provider-singlestore.xpkg
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="${repo_root}/package/crds"
dst="${repo_root}/package/singlestore/crds"

shopt -s nullglob
crds=("${src}"/singlestore.sql.crossplane.io_*.yaml)
if [ ${#crds[@]} -eq 0 ]; then
  echo "no singlestore CRDs in ${src}; run 'make generate' first" >&2
  exit 1
fi

rm -rf "${dst}"
mkdir -p "${dst}"
cp "${crds[@]}" "${dst}/"
echo "staged ${#crds[@]} singlestore CRD(s) into ${dst}:"
printf '  %s\n' "${crds[@]##*/}"
