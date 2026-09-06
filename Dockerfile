# syntax=docker/dockerfile:1.7
FROM golang:1.26.5 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG COMMAND=api

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${COMMAND}

FROM scratch
USER 65532:65532
COPY --from=build /out/service /service
EXPOSE 8080
ENTRYPOINT ["/service"]
