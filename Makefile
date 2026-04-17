.PHONY: run build build-arm64

run: build
	./bin/main

build:
	go build -o bin/main main.go

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/main_arm64 main.go