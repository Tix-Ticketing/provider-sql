# Self-contained multi-stage build of the provider-sql controller for CI
# (buildkit in Argo Workflows). Unlike cluster/images/provider-sql/Dockerfile —
# which expects `make` to have cross-compiled the binary first — this compiles
# the binary itself, so `buildctl ... --frontend dockerfile.v0` can build it
# straight from a git checkout with no prior steps.
#
# The resulting image is the runtime embedded into the provider-singlestore xpkg
# (crossplane xpkg build --embed-runtime-image-tarball). Same binary as the full
# provider; the package is gated to FLAVOURS=singlestore at install time.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
RUN apk add --no-cache git ca-certificates

# Cache modules separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/provider ./cmd/provider

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
COPY --from=build /out/provider /usr/local/bin/crossplane-sql-provider
USER 65532
ENTRYPOINT ["crossplane-sql-provider"]
