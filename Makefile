VERSION ?= dev
NPM_VERSION ?= 0.0.0-local

.PHONY: build test cover lint fmt docker run clean npm-build npm-test npm-publish-dry

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
	rm -rf bin/ dist/ npm/build/ coverage.out coverage.html

# Stage a single-platform npm package using the locally-built binary
# and the current host's platform. End-to-end exercises build.mjs without
# needing a full goreleaser run.
npm-build: build
	mkdir -p dist/llm-lint_linux_amd64
	cp bin/llm-lint dist/llm-lint_linux_amd64/llm-lint
	node npm/scripts/build.mjs --version $(NPM_VERSION) --only linux-x64

# Pack the built packages and run the shim through `npx` against the
# packed tarballs. Verifies the optionalDependencies resolution works.
npm-test: npm-build
	@bash -c 'set -euo pipefail; \
		cd npm/build/llm-lint-linux-x64 && npm pack --silent >/dev/null; \
		cd ../llm-lint && npm pack --silent >/dev/null; \
		td=$$(mktemp -d); \
		cd "$$td"; \
		printf "{\"name\":\"t\",\"version\":\"0\",\"private\":true,\"dependencies\":{\"@jadenrazo/llm-lint\":\"file:$(CURDIR)/npm/build/llm-lint/jadenrazo-llm-lint-$(NPM_VERSION).tgz\",\"@jadenrazo/llm-lint-linux-x64\":\"file:$(CURDIR)/npm/build/llm-lint-linux-x64/jadenrazo-llm-lint-linux-x64-$(NPM_VERSION).tgz\"}}" > package.json; \
		npm install --silent --no-audit --no-fund >/dev/null; \
		echo "--- llm-lint version (via shim) ---"; \
		./node_modules/.bin/llm-lint version; \
		echo "--- llm-lint rules | wc -l ---"; \
		./node_modules/.bin/llm-lint rules | wc -l; \
		rm -rf "$$td"'

# Dry-run the publish to confirm tarball contents without uploading.
npm-publish-dry: npm-build
	node npm/scripts/publish.mjs --version $(NPM_VERSION) --dry-run
