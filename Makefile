.PHONY: build test race docker-build

build:
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/shell-tool ./cmd/shell-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/shell-tool-console ./cmd/shell-tool-console

test:
	go test ./...

race:
	go test -race ./...

docker-build:
	docker build -t agent-shell-tool:dev .