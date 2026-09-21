# syntax=docker/dockerfile:1

FROM node:24.19.0-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable && corepack prepare pnpm@11.22.0 --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.26.5-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ internal/webui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl \
	&& rm -rf /var/lib/apt/lists/*
COPY --from=build /out/server /server
EXPOSE 8300
USER nobody
HEALTHCHECK --interval=5s --timeout=3s --retries=10 --start-period=20s \
	CMD curl -fsS http://127.0.0.1:8300/healthz >/dev/null || exit 1
ENTRYPOINT ["/server"]
