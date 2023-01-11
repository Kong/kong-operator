# ------------------------------------------------------------------------------
# Debug image
# ------------------------------------------------------------------------------

FROM golang:1.19.5 as debug

ARG TAG
ARG NAME="Kong Gateway Operator"
ARG DESCRIPTION="Kong Gateway Operator debug image"
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
      url="https://github.com/Kong/gateway-operator" \
      summary="A Kubernetes Operator for the Kong Gateway."

RUN printf "Building for TARGETPLATFORM=${TARGETPLATFORM}" \
    && printf ", TARGETARCH=${TARGETARCH}" \
    && printf ", TARGETOS=${TARGETOS}" \
    && printf ", TARGETVARIANT=${TARGETVARIANT} \n" \
    && printf "With 'uname -s': $(uname -s) and 'uname -m': $(uname -m)"

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY Makefile Makefile
COPY third_party/go.mod third_party/go.mod
COPY third_party/go.sum third_party/go.sum
COPY third_party/dlv.go third_party/dlv.go
RUN make dlv

COPY main.go main.go
COPY apis/ apis/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY internal/ internal/
COPY .git/ .git/

RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    TAG="${TAG}" COMMIT="${COMMIT}" REPO_INFO="${REPO_INFO}" \
    make build.operator.debug && \
    mv ./bin/manager /go/bin/manager && \
    mv ./bin/dlv /go/bin/dlv

ENTRYPOINT [ "/go/bin/dlv" ]
CMD [ "--continue", "--accept-multiclient", "--listen=:40000", "--check-go-version=false", "--headless=true", "--api-version=2", "--log=true", "--log-output=debugger,debuglineerr,gdbwire", "exec", "/go/bin/manager", "--" ]
