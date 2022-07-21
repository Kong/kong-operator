FROM golang:1.18 as builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY main.go main.go
COPY apis/ apis/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

### Distroless/default
# Use distroless as minimal base image to package the operator binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot as distroless

ARG TAG

LABEL name="Kong Ingress Controller" \
      vendor="Kong" \
      version="$TAG" \
      release="1" \
      url="https://github.com/Kong/gateway-operator" \
      summary="A Kubernetes Operator for the Kong Gateway." \
      description="Kong Gateway Operator drives deployment via the Gateway resource. You can deploy a Gateway resource to the cluster which will result in the underlying control-plane (the Kong Kubernetes Ingress Controller) and the data-plane (the Kong Gateway)."

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532



ENTRYPOINT ["/manager"]

### RHEL
# Build UBI image
FROM registry.access.redhat.com/ubi8/ubi AS redhat

ARG TAG
ARG NAME="Kong Gateway Operator"
ARG DESCRIPTION="Kong Gateway Operator drives deployment via the Gateway resource. You can deploy a Gateway resource to the cluster which will result in the underlying control-plane (the Kong Kubernetes Ingress Controller) and the data-plane (the Kong Gateway)."


LABEL name="$NAME" \
      io.k8s.display-name="$NAME" \ 
      vendor="Kong" \
      version="$TAG" \
      release="1" \
      url="https://github.com/Kong/gateway-operator" \
      summary="A Kubernetes Operator for the Kong Gateway." \
      description="$DESCRIPTION" \
      io.k8s.description="$DESCRIPTION"


# Create the user (ID 1000) and group that will be used in the
# running container to run the process as an unprivileged user.
RUN groupadd --system gateway-operator && \
    adduser --system gateway-operator -g gateway-operator -u 1000

COPY --from=builder /workspace/manager .
COPY LICENSE /licenses/

# Perform any further action as an unprivileged user.
USER 1000

# Run the compiled binary.
ENTRYPOINT ["/manager"]
