.PHONY: build test race vet format-check run worker benchmark

build:
	go build ./cmd/...

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

format-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

run:
	GITHUB_WEBHOOK_SECRET=local-development-secret go run ./cmd/api

worker:
	go run ./cmd/worker -trusted=true

benchmark:
	go test -bench=. -benchmem ./benchmarks
