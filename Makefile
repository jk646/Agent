.PHONY: build test race docker-build

build:
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/shell-tool ./cmd/shell-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/shell-tool-console ./cmd/shell-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/file-tool ./cmd/file-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/file-tool-console ./cmd/file-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/file-search-tool ./cmd/file-search-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/file-search-tool-console ./cmd/file-search-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/read-file-tool ./cmd/read-file-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/read-file-tool-console ./cmd/read-file-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/read-folder-tool ./cmd/read-folder-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/read-folder-tool-console ./cmd/read-folder-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/search-text-tool ./cmd/search-text-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/search-text-tool-console ./cmd/search-text-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/write-file-tool ./cmd/write-file-tool
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/write-file-tool-console ./cmd/write-file-tool-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/agent-orchestrator ./cmd/agent-orchestrator
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/agent-orchestrator-console ./cmd/agent-orchestrator-console
	CGO_ENABLED=0 GOOS=linux go build -trimpath -o bin/agent-orchestrator-load ./cmd/agent-orchestrator-load

test:
	go test ./...

race:
	go test -race ./...

docker-build:
	docker build -t agent-shell-tool:dev .
