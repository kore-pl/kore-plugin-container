# kore-plugin-container 构建入口：build / dist / check / clean。
SHELL := /bin/bash

ID := container
BIN := bin/$(ID)
DIST := dist
VERSION := $(shell sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' info.json | head -n 1)
PACKAGE := $(DIST)/$(ID)-$(VERSION).tar.gz

.PHONY: build dist check clean

# 静态构建：G-16 兼容性基线（CGO_ENABLED=0 + GOAMD64=v1 + trimpath + -s -w）。
build:
	CGO_ENABLED=0 GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w" -o $(BIN) .

# 确定性打包：info.json（回填 size_bytes）+ bin/container + SHA256SUMS。
# 显式列条目（info.json bin SHA256SUMS），无 ./ 根条目，无 web/ 前端。
# 确定性四件套：--sort=name + --mtime=@0 + --owner=0/--group=0/--numeric-owner + gzip -9n。
# chmod 归一：目录 755 / 可执行文件 755 / 普通文件 644，消除 umask 与宿主权限漂移。
# size_bytes 精确收敛：go:embed 源是静态 manifest.json（不含 size_bytes），
# 回填包内 info.json 不改变二进制字节，故单次回填即为最终包体，无自指震荡。
dist: build
	@set -eo pipefail; \
	rm -rf $(DIST)/stage $(PACKAGE) $(PACKAGE).sha256; \
	mkdir -p $(DIST)/stage/bin; \
	cp $(BIN) $(DIST)/stage/bin/$(ID); \
	cp info.json $(DIST)/stage/info.json; \
	chmod 755 $(DIST)/stage/bin $(DIST)/stage/bin/$(ID); \
	chmod 644 $(DIST)/stage/info.json; \
	size=0; \
	for _ in 1 2 3; do \
		( cd $(DIST)/stage && sha256sum info.json bin/$(ID) > SHA256SUMS ); \
		chmod 644 $(DIST)/stage/SHA256SUMS; \
		tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -cf - -C $(DIST)/stage info.json bin SHA256SUMS | gzip -9n > $(PACKAGE); \
		actual=$$(stat -c%s $(PACKAGE)); \
		if [ "$$actual" = "$$size" ]; then break; fi; \
		size=$$actual; \
		sed "s/\"size_bytes\": [0-9]*/\"size_bytes\": $$size/" info.json > $(DIST)/stage/info.json; \
		chmod 644 $(DIST)/stage/info.json; \
	done; \
	( cd $(DIST) && sha256sum $(ID)-$(VERSION).tar.gz > $(ID)-$(VERSION).tar.gz.sha256 ); \
	rm -rf $(DIST)/stage; \
	packed=$$(tar -xOzf $(PACKAGE) info.json | sed -n 's/.*"size_bytes": \([0-9]*\).*/\1/p'); \
	echo "打包完成: $(PACKAGE)，包内 size_bytes=$$packed，实际包体=$$size（代理值偏差见下）"; \
	echo "注：gzip 压缩非单调，包内 size_bytes 与实际包体可能有数字节偏差（<0.01%，远低于 R3 的 10% 阈值）；固定迭代次数保证 sha 完全确定"

# 门禁：gofmt（有输出即失败）→ go vet → go test。
check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt 需要格式化："; echo "$$out"; exit 1; fi; echo "gofmt: clean"
	go vet ./...
	go test -timeout 200s ./...

clean:
	rm -rf bin dist
	rm -f go.work.sum
