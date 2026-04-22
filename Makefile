.PHONY: run build build-arm64-linux build-amd64-linux

run: build
	./bin/main

build:
	rm -rf bin/
	go build -o bin/main main.go

build-arm64-linux:
		rm -rf bin/
		docker run --rm \
		-v $(CURDIR):/app \
		-v /tmp/go-cache:/go/pkg/mod \
		-w /app \
		--platform linux/arm64 \
		-e CGO_ENABLED=1 \
		-e GOPROXY=https://goproxy.cn,direct \
		golang:1.26.2 go build -o bin/main_arm64_linux main.go

build-amd64-linux:
		rm -rf bin/
		docker run --rm \
		-v $(CURDIR):/app \
		-v /tmp/go-cache:/go/pkg/mod \
		-w /app \
		--platform linux/amd64 \
		-e CGO_ENABLED=1 \
		-e GOPROXY=https://goproxy.cn,direct \
		golang:1.26.2 go build -o bin/main_amd64_linux main.go