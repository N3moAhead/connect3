.DEFAULT_GOAL := run

fmt:
	go fmt ./...

vet: fmt
	go vet ./...

build: vet
	go build -o c3-local ./cmd/connect3/main.go

run: build
	# While testing use the local data.json file
	./c3-local --db data.json
clean:
	rm c3-local

reset:
	rm data.json
