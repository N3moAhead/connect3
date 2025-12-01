.DEFAULT_GOAL := run

fmt:
	go fmt ./...

vet: fmt
	go vet ./...

build: vet
	go build -o main ./cmd/connect3/main.go

run: build
	./main

clean:
	rm main

reset:
	rm data.json
