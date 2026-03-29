.PHONY: build clean

BINARY_NAME=podman-volume-porter

build:
	@echo "正在编译静态文件..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ./dist/$(BINARY_NAME) main.go
