VERSION ?= dev

.PHONY: build test cover lint fmt docker run clean

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/llm-lint ./cmd/llm-lint

test:
	go test -race -coverprofile=coverage.out ./...

cover: test
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed; run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; exit 1; }
	golangci-lint run

fmt:
	gofmt -s -w .
	go mod tidy

docker:
	docker build -t llm-lint:$(VERSION) --build-arg VERSION=$(VERSION) .

run: build
	./bin/llm-lint scan

clean:
	rm -rf bin/ dist/ coverage.out coverage.html
