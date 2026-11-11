# syntax=docker/dockerfile:1.7
# Build from the repository root:
#   docker buildx build -f deploy/docker/Dockerfile -t ghcr.io/prat31/airval .
# Targets: airval (default), mocktarget (fake AI service for demos and benchmarks).

ARG NODE_VERSION=lts
ARG GO_VERSION=1.27
ARG DISTROLESS=gcr.io/distroless/static:nonroot

# ---- web: build the UI that gets embedded into the binary --------------------------------------
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
COPY api/ /src/api/
RUN npm run build

# ---- build: static, CGO-free Go binaries ---------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG TAGS=""
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags "$TAGS" \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/airval ./cmd/airval && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags "-s -w" -o /out/mocktarget ./tools/mocktarget && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags "-s -w" -o /out/airval-probe ./deploy/docker/probe && \
    mkdir -p /out/data

# ---- mocktarget: fake AI service (docker build --target mocktarget) --------------------------
FROM ${DISTROLESS} AS mocktarget
COPY --from=build /out/mocktarget /usr/local/bin/mocktarget
USER 65532:65532
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/mocktarget"]

# ---- airval: the product image -------------------------------------------------------------------
FROM ${DISTROLESS} AS airval
ARG VERSION=dev
ARG COMMIT=unknown
ARG CREATED=""
LABEL org.opencontainers.image.title="airval" \
      org.opencontainers.image.description="Self-hostable evaluation platform for AI services" \
      org.opencontainers.image.url="https://github.com/prat31/airval" \
      org.opencontainers.image.source="https://github.com/prat31/airval" \
      org.opencontainers.image.documentation="https://github.com/prat31/airval#readme" \
      org.opencontainers.image.vendor="airval" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${CREATED}"
COPY --from=build /out/airval /out/airval-probe /usr/local/bin/
# State dir for `airval dev` (relative ./airval-data) and the embedded NATS store; mount a volume here.
COPY --from=build --chown=65532:65532 /out/data /data
WORKDIR /data
VOLUME ["/data"]
USER 65532:65532
EXPOSE 8080
ENV AIRVAL_SERVER_ADDR=":8080"
# Distroless has no shell: healthcheck with ["CMD", "/usr/local/bin/airval-probe"].
ENTRYPOINT ["/usr/local/bin/airval"]
CMD ["server"]
