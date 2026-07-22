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

test:
	go test ./...

race:
	go test -race ./...

docker-build:
	docker build -t agent-shell-tool:dev .
