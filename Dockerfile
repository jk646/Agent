FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shell-tool ./cmd/shell-tool \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shell-tool-console ./cmd/shell-tool-console \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/file-tool ./cmd/file-tool \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/file-tool-console ./cmd/file-tool-console \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/file-search-tool ./cmd/file-search-tool \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/file-search-tool-console ./cmd/file-search-tool-console \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/read-file-tool ./cmd/read-file-tool \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/read-file-tool-console ./cmd/read-file-tool-console \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/read-folder-tool ./cmd/read-folder-tool \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/read-folder-tool-console ./cmd/read-folder-tool-console \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/search-text-tool ./cmd/search-text-tool \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/search-text-tool-console ./cmd/search-text-tool-console \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/write-file-tool ./cmd/write-file-tool \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/write-file-tool-console ./cmd/write-file-tool-console \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-orchestrator ./cmd/agent-orchestrator \
	&& CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent-orchestrator-console ./cmd/agent-orchestrator-console

FROM debian:bookworm-slim
RUN useradd --create-home --uid 10001 agent \
    && mkdir -p /run/agent /tmp/agent-shell /tmp/agent-file-tool /tmp/agent-write-file /etc/agent /workspace \
    && chown -R agent:agent /run/agent /tmp/agent-shell /tmp/agent-file-tool /tmp/agent-write-file /workspace
COPY --from=build /out/shell-tool /usr/local/bin/shell-tool
COPY --from=build /out/shell-tool-console /usr/local/bin/shell-tool-console
COPY --from=build /out/file-tool /usr/local/bin/file-tool
COPY --from=build /out/file-tool-console /usr/local/bin/file-tool-console
COPY --from=build /out/file-search-tool /usr/local/bin/file-search-tool
COPY --from=build /out/file-search-tool-console /usr/local/bin/file-search-tool-console
COPY --from=build /out/read-file-tool /usr/local/bin/read-file-tool
COPY --from=build /out/read-file-tool-console /usr/local/bin/read-file-tool-console
COPY --from=build /out/read-folder-tool /usr/local/bin/read-folder-tool
COPY --from=build /out/read-folder-tool-console /usr/local/bin/read-folder-tool-console
COPY --from=build /out/search-text-tool /usr/local/bin/search-text-tool
COPY --from=build /out/search-text-tool-console /usr/local/bin/search-text-tool-console
COPY --from=build /out/write-file-tool /usr/local/bin/write-file-tool
COPY --from=build /out/write-file-tool-console /usr/local/bin/write-file-tool-console
COPY --from=build /out/agent-orchestrator /usr/local/bin/agent-orchestrator
COPY --from=build /out/agent-orchestrator-console /usr/local/bin/agent-orchestrator-console
COPY configs/orchestrator-tools.json /etc/agent/orchestrator-tools.json
USER agent
WORKDIR /workspace
ENV SHELL_TOOL_SOCKET=/run/agent/shell-tool.sock
ENV FILE_TOOL_SOCKET=/run/agent/file-tool.sock
ENV FILE_TOOL_WORKSPACE=/workspace
ENV FILE_SEARCH_TOOL_SOCKET=/run/agent/file-search-tool.sock
ENV FILE_SEARCH_TOOL_WORKSPACE=/workspace
ENV READ_FILE_TOOL_SOCKET=/run/agent/read-file-tool.sock
ENV READ_FILE_TOOL_WORKSPACE=/workspace
ENV READ_FOLDER_TOOL_SOCKET=/run/agent/read-folder-tool.sock
ENV READ_FOLDER_TOOL_WORKSPACE=/workspace
ENV SEARCH_TEXT_TOOL_SOCKET=/run/agent/search-text-tool.sock
ENV SEARCH_TEXT_TOOL_WORKSPACE=/workspace
ENV WRITE_FILE_TOOL_SOCKET=/run/agent/write-file-tool.sock
ENV WRITE_FILE_TOOL_WORKSPACE=/workspace
ENV WRITE_FILE_TOOL_TEMP_DIR=/tmp/agent-write-file
ENV AGENT_ORCHESTRATOR_SOCKET=/run/agent/orchestrator.sock
ENV AGENT_ORCHESTRATOR_TOOLS_FILE=/etc/agent/orchestrator-tools.json
ENTRYPOINT ["/usr/local/bin/shell-tool"]
