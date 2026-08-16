# Argus 生产镜像（多阶段构建）
# 阶段 1：构建前端
FROM docker.io/library/node:22-alpine AS web
ENV CI=true
RUN corepack enable
WORKDIR /build/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 阶段 2：构建后端（内嵌前端）
FROM docker.io/library/golang:1.26-alpine AS gobuild
WORKDIR /build
COPY protocol/ protocol/
COPY server/ server/
COPY --from=web /build/web/dist /build/server/cmd/argus-server/webdist
RUN cd server && go build -o /argus-server ./cmd/argus-server

# 阶段 3：运行
FROM docker.io/library/alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=gobuild /argus-server /usr/local/bin/argus-server
EXPOSE 8080 9090
VOLUME ["/app/data"]
ENV ARGUS_LISTEN=0.0.0.0:8080
ENV ARGUS_DB=/app/data/argus.db
ENTRYPOINT ["argus-server"]
