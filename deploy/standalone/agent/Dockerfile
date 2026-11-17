# syntax=docker/dockerfile:1
FROM golang:1.26.10-bookworm AS agent-build
WORKDIR /src
# Portainer Agent CE 2.45.3, pinned independently from the server working tree.
RUN git clone https://github.com/portainer/agent.git agent && cd agent && git checkout --detach 6a2ec70051bccf7f5c429c43a8112692230991d3
COPY . /src/server-ce
COPY deploy/standalone/agent/local.go.overlay /src/agent/http/proxy/local.go
COPY deploy/standalone/agent/local_upgrade_test.go.overlay /src/agent/http/proxy/local_upgrade_test.go
WORKDIR /src/agent
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go test -mod=readonly ./http/proxy -run TestLocalProxyUpgrade
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} go build -mod=readonly -trimpath -ldflags='-s -w' -o /out/agent ./cmd/agent

FROM portainer/agent:2.45.3
COPY --from=agent-build /out/agent /app/agent
LABEL org.opencontainers.image.title="Portainer Agent CE 2.45.3 with bidirectional Docker upgrades for managed builds"
