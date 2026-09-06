# syntax=docker/dockerfile:1.7
FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/worker && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/controller ./cmd/controller

FROM alpine:3.22 AS kubectl
ARG TARGETARCH=amd64
ARG KUBECTL_VERSION=v1.35.8
RUN apk add --no-cache curl ca-certificates && \
    curl --fail --retry 3 -L "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" -o /kubectl && \
    curl --fail --retry 3 -L "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl.sha256" -o /kubectl.sha256 && \
    echo "$(cat /kubectl.sha256)  /kubectl" | sha256sum -c - && chmod 0755 /kubectl

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /
COPY --from=kubectl /kubectl /usr/local/bin/kubectl
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/api"]
