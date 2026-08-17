# Argus 构建与部署

GO ?= go
PNPM ?= pnpm
VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: all web server agent build run mock release clean

all: build

## 构建前端产物
web:
	cd web && $(PNPM) install && $(PNPM) build
	rm -rf server/cmd/argus-server/webdist
	mkdir -p server/cmd/argus-server/webdist
	cp -r web/dist/* server/cmd/argus-server/webdist/

## 构建 server（内嵌前端）
server:
	cd server && $(GO) build -o argus-server ./cmd/argus-server

## 构建 agent
agent:
	cd agent && $(GO) build -o argus-agent ./cmd/argus-agent

## 一键构建（web → 内嵌 → server + agent）
build: web server agent

## 本地运行（需先 build）
run:
	./server/argus-server -l 0.0.0.0:8080

## 前端演示模式（无需后端）
mock:
	cd web && $(PNPM) dev:mock

## 发布构建：多平台二进制 + SHA-256（不创建 tag/release、不推送）
## 用法：make release [VERSION=x.y.z]（默认取当前 commit 短哈希）
release: web
	ARGUS_RELEASE_VERSION="$(VERSION)" bash scripts/release-build.sh

clean:
	rm -f server/argus-server agent/argus-agent
	rm -rf web/dist server/cmd/argus-server/webdist
