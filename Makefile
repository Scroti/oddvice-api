.PHONY: run build test tidy fmt vet lint clean docker

run:
	go run ./cmd/server

build:
	go build -ldflags="-s -w" -o bin/server ./cmd/server

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin

docker:
	docker build -t oddvice-api .
