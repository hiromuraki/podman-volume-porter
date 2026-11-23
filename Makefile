.PHONY: build clean

BINARY_NAME=podman-volume-porter
VERSION ?= dev

build:
	@echo "正在编译静态文件 ($(VERSION))..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o ./dist/$(BINARY_NAME) .

clean:
	rm -rf ./dist
