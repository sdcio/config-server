# Copyright 2022 Nokia
# Licensed under the Apache License 2.0
# SPDX-License-Identifier: Apache-2.0

# Build the manager binary
FROM golang:1.21 as builder
ARG TARGETOS
ARG TARGETARCH

RUN apt-get update && apt-get install -y ca-certificates git-core ssh tzdata
RUN mkdir -p -m 0700 /root/.ssh && ssh-keyscan github.com >> /root/.ssh/known_hosts
RUN git config --global url.ssh://git@github.com/.insteadOf https://github.com/

# add unprivileged user
RUN adduser --shell /usr/sbin/nologin --uid 1000 --disabled-login --no-create-home --gecos '' app \
    && sed -i -r "/^(app|root)/!d" /etc/group /etc/passwd \
    && sed -i -r 's#^(.*):[^:]*$#\1:/usr/sbin/nologin#' /etc/passwd

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=ssh \
    go mod download

# Copy the go source
COPY cmd/ cmd/
COPY apis/ apis/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=ssh \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o config-server cmd/apiserver/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
#FROM gcr.io/distroless/static:nonroot
#FROM alpine:latest
FROM scratch
# add-in our timezone data file
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
# add-in our nologin binary
COPY --from=builder /usr/sbin/nologin /usr/sbin/nologin
# add-in our unprivileged user
COPY --from=builder /etc/passwd /etc/group /etc/shadow /etc/
# add-in our ca certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
#
COPY --from=builder /workspace/config-server /app/
#USER 65532:65532
WORKDIR /app
# from now on, run as the unprivileged user
USER 1000
ENTRYPOINT ["/app/config-server"]
