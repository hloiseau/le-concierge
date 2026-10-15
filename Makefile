.PHONY: test build fmt vet run-api run-bot
test:
	go test -race ./...
vet:
	go vet ./...
fmt:
	gofmt -w cmd internal
build:
	go build -o bin/api ./cmd/api
	go build -o bin/bot ./cmd/bot
	go build -o bin/manage ./cmd/manage
run-api:
	go run ./cmd/api
run-bot:
	go run ./cmd/bot
