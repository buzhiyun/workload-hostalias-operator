# Build the manager binary
FROM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

# Use China mirror for Go modules
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that changes in the source code don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o /manager cmd/main.go

# Use alpine as minimal base image
FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /manager .
USER 65534:65534

ENTRYPOINT ["/manager"]
