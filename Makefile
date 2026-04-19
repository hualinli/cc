.PHONY: run build build-arm64

run: build
	./bin/main

build:
	go build -o bin/main main.go

build-arm64:
		docker run --rm \
		-v $(CURDIR):/app \
		-v /tmp/go-cache:/go/pkg/mod \
		-w /app \
		--platform linux/arm64 \
		-e CGO_ENABLED=1 \
		-e GOPROXY=https://goproxy.cn,direct \
		golang:1.26 go build -o bin/main_arm64 main.go
