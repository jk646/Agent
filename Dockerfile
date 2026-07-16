FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shell-tool ./cmd/shell-tool \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/shell-tool-console ./cmd/shell-tool-console

FROM debian:bookworm-slim
RUN useradd --create-home --uid 10001 agent \
    && mkdir -p /run/agent /tmp/agent-shell /workspace \
    && chown -R agent:agent /run/agent /tmp/agent-shell /workspace
COPY --from=build /out/shell-tool /usr/local/bin/shell-tool
COPY --from=build /out/shell-tool-console /usr/local/bin/shell-tool-console
USER agent
WORKDIR /workspace
ENV SHELL_TOOL_SOCKET=/run/agent/shell-tool.sock
ENTRYPOINT ["/usr/local/bin/shell-tool"]