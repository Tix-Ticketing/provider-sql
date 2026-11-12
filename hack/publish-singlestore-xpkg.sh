#!/usr/bin/env bash
# Build and publish the provider-singlestore Crossplane package to ECR.
#
# Local equivalent of the provider-xpkg-build Argo WorkflowTemplate, for the
# first publish / ad-hoc builds. Steps: compile the controller, build its OCI
# image, stage the singlestore CRDs, build the xpkg with the runtime embedded,
# then push the xpkg to ECR (creating the repo if absent).
#
# Usage:
#   AWS_PROFILE=tix-shared hack/publish-singlestore-xpkg.sh [TAG]
#
# Env (with defaults):
#   REGISTRY  352695479602.dkr.ecr.eu-west-1.amazonaws.com   (tix-shared, eu-west-1)
#   REPO      provider-singlestore
#   REGION    eu-west-1
#   TAG       arg $1 or v0.1.0
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

REGISTRY="${REGISTRY:-352695479602.dkr.ecr.eu-west-1.amazonaws.com}"
REPO="${REPO:-provider-singlestore}"
REGION="${REGION:-eu-west-1}"
TAG="${1:-${TAG:-v0.1.0}}"
PROFILE_ARG=()
[ -n "${AWS_PROFILE:-}" ] && PROFILE_ARG=(--profile "${AWS_PROFILE}")

RUNTIME_IMG="provider-singlestore-runtime:${TAG}"
XPKG_IMG="${REGISTRY}/${REPO}:${TAG}"
XPKG_FILE="${repo_root}/_output/provider-singlestore-${TAG}.xpkg"

echo ">> 1/5 build controller image (${RUNTIME_IMG})"
docker build --platform linux/amd64 \
  -f cluster/images/provider-singlestore/Dockerfile \
  -t "${RUNTIME_IMG}" .

echo ">> 2/5 stage singlestore CRDs"
hack/stage-singlestore-pkg.sh

echo ">> 3/5 build xpkg (${XPKG_FILE})"
mkdir -p "$(dirname "${XPKG_FILE}")"
rm -f "${XPKG_FILE}"
crossplane xpkg build \
  --package-root package/singlestore \
  --embed-runtime-image "${RUNTIME_IMG}" \
  --package-file "${XPKG_FILE}"

echo ">> 4/5 ensure ECR repo + login (${REGISTRY}/${REPO})"
if ! aws ecr describe-repositories --repository-names "${REPO}" \
      --region "${REGION}" "${PROFILE_ARG[@]}" >/dev/null 2>&1; then
  echo "   creating ECR repository ${REPO}"
  aws ecr create-repository --repository-name "${REPO}" \
    --image-scanning-configuration scanOnPush=true \
    --image-tag-mutability MUTABLE \
    --region "${REGION}" "${PROFILE_ARG[@]}" >/dev/null
fi
# crossplane xpkg push reads the docker keychain (~/.docker/config.json).
aws ecr get-login-password --region "${REGION}" "${PROFILE_ARG[@]}" \
  | docker login --username AWS --password-stdin "${REGISTRY}"

echo ">> 5/5 push ${XPKG_IMG}"
crossplane xpkg push --package-files "${XPKG_FILE}" "${XPKG_IMG}"
echo "published ${XPKG_IMG}"
