# ------------------------------------------------------------------------------
# Builder
# ------------------------------------------------------------------------------

FROM golang:1.19.3 as builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TAG
ARG COMMIT
ARG REPO_INFO

RUN printf "Building for TARGETPLATFORM=${TARGETPLATFORM}" \
    && printf ", TARGETARCH=${TARGETARCH}" \
    && printf ", TARGETOS=${TARGETOS}" \
    && printf ", TARGETVARIANT=${TARGETVARIANT} \n" \
    && printf "With 'uname -s': $(uname -s) and 'uname -m': $(uname -m)"

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY main.go main.go
COPY apis/ apis/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY internal/ internal/
COPY Makefile Makefile
COPY .git/ .git/

RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    TAG="${TAG}" COMMIT="${COMMIT}" REPO_INFO="${REPO_INFO}" \
    make build.operator

# ------------------------------------------------------------------------------
# Distroless (default)
# ------------------------------------------------------------------------------

# Use distroless as minimal base image to package the operator binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot as distroless

ARG TAG
ARG NAME="Kong Gateway Operator"
ARG DESCRIPTION="Kong Gateway Operator drives deployment via the Gateway resource. You can deploy a Gateway resource to the cluster which will result in the underlying control-plane (the Kong Kubernetes Ingress Controller) and the data-plane (the Kong Gateway)."

LABEL name="${NAME}" \
      description="${DESCRIPTION}" \
      org.opencontainers.image.description="${DESCRIPTION}" \
      vendor="Kong" \
      version="${TAG}" \
      release="1" \
      url="https://github.com/Kong/gateway-operator" \
      summary="A Kubernetes Operator for the Kong Gateway."

WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

# ------------------------------------------------------------------------------
# RedHat UBI
# ------------------------------------------------------------------------------

FROM registry.access.redhat.com/ubi8/ubi AS redhat

ARG TAG
ARG NAME="Kong Gateway Operator"
ARG DESCRIPTION="Kong Gateway Operator drives deployment via the Gateway resource. You can deploy a Gateway resource to the cluster which will result in the underlying control-plane (the Kong Kubernetes Ingress Controller) and the data-plane (the Kong Gateway)."

LABEL name="${NAME}" \
      io.k8s.display-name="${NAME}" \
      description="${DESCRIPTION}" \
      io.k8s.description="${DESCRIPTION}" \
      org.opencontainers.image.description="${DESCRIPTION}" \
      vendor="Kong" \
      version="${TAG}" \
      release="1" \
      url="https://github.com/Kong/gateway-operator" \
      summary="A Kubernetes Operator for the Kong Gateway."

# Create the user (ID 1000) and group that will be used in the
# running container to run the process as an unprivileged user.
RUN groupadd --system gateway-operator && \
    adduser --system gateway-operator -g gateway-operator -u 1000

COPY --from=builder /workspace/bin/manager .
COPY LICENSE /licenses/

# Perform any further action as an unprivileged user.
USER 1000

# Run the compiled binary.
ENTRYPOINT ["/manager"]
