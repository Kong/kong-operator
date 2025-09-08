# ------------------------------------------------------------------------------
# Debug image
# ------------------------------------------------------------------------------

FROM --platform=$BUILDPLATFORM golang:1.25.1@sha256:a5e935dbd8bc3a5ea24388e376388c9a69b40628b6788a81658a801abbec8f2e AS debug

ARG GOPATH
ARG GOCACHE

ARG TAG
ARG NAME="Kong Operator"
ARG DESCRIPTION="Kong Operator debug image"
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG COMMIT
ARG REPO_INFO

LABEL name="${NAME}" \
    description="${DESCRIPTION}" \
    org.opencontainers.image.description="${DESCRIPTION}" \
    vendor="Kong" \
    version="${TAG}" \
    release="1" \
    url="https://github.com/kong/kong-operator" \
    summary="A Kubernetes Operator for the Kong Gateway."

RUN printf "Building for TARGETPLATFORM=${TARGETPLATFORM}" \
    && printf ", TARGETARCH=${TARGETARCH}" \
    && printf ", TARGETOS=${TARGETOS}" \
    && printf ", TARGETVARIANT=${TARGETVARIANT} \n" \
    && printf "With 'uname -s': $(uname -s) and 'uname -m': $(uname -m)"

WORKDIR /workspace

RUN --mount=type=cache,target=$GOPATH/pkg/mod \
    --mount=type=cache,target=$GOCACHE \
    go install github.com/go-delve/delve/cmd/dlv@v1.24.0

# Use cache mounts to cache Go dependencies and bind mounts to avoid unnecessary
# layers when using COPY instructions for go.mod and go.sum.
# https://docs.docker.com/build/guide/mounts/
RUN --mount=type=cache,target=$GOPATH/pkg/mod \
    --mount=type=cache,target=$GOCACHE \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

COPY ingress-controller/ ingress-controller/
COPY cmd/main.go cmd/main.go
COPY modules/ modules/
COPY controller/ controller/
COPY pkg/ pkg/
COPY internal/ internal/
COPY Makefile Makefile
COPY .git/ .git/

# Use cache mounts to cache Go dependencies and bind mounts to avoid unnecessary
# layers when using COPY instructions for go.mod and go.sum.
# https://docs.docker.com/build/guide/mounts/
RUN --mount=type=cache,target=$GOPATH/pkg/mod \
    --mount=type=cache,target=$GOCACHE \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    TAG="${TAG}" COMMIT="${COMMIT}" REPO_INFO="${REPO_INFO}" \
    make build.operator.debug && \
    mv ./bin/manager /go/bin/manager

ENTRYPOINT [ "dlv" ]
CMD [ "--continue", "--accept-multiclient", "--listen=:40000", "--check-go-version=false", "--headless=true", "--api-version=2", "--log=true", "--log-output=debugger,debuglineerr,gdbwire", "exec", "/go/bin/manager", "--" ]
