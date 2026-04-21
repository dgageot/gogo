# syntax=docker/dockerfile:1

ARG GO_VERSION="1.26.2"
ARG ALPINE_VERSION="3.22"
ARG XX_VERSION="1.9.0"

# xx is a helper for cross-compilation
FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

# osxcross contains the MacOSX cross toolchain for xx
FROM crazymax/osxcross:15.5-debian AS osxcross

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder-base
COPY --from=xx / /
RUN apk add --no-cache clang zig musl-dev gcc
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download
ENV CGO_ENABLED=1

FROM builder-base AS builder
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/var/cache/apk,id=apk-$TARGETPLATFORM,sharing=locked \
    xx-apk add musl-dev gcc
COPY . ./
RUN --mount=type=bind,from=osxcross,src=/osxsdk,target=/xx-sdk \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETPLATFORM \
    --mount=type=cache,target=/go/pkg/mod <<EOT
    set -ex
    if [ "$TARGETOS" = "darwin" ]; then
      export CGO_ENABLED=1
    else
      export CGO_ENABLED=0
    fi
    xx-go build -trimpath -ldflags "-s -w" -o /binaries/gogo-$TARGETOS-$TARGETARCH .
    xx-verify /binaries/gogo-$TARGETOS-$TARGETARCH
EOT

FROM scratch AS cross
COPY --from=builder /binaries .

FROM scratch AS local
ARG TARGETOS TARGETARCH
COPY --from=builder /binaries/gogo-$TARGETOS-$TARGETARCH gogo
