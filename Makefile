.PHONY: run-main build-main build-main-arm64

run-main: build-main
	./bin/main

build-main:
	go build -o bin/main main.go

build-main-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/main_arm64 main.go