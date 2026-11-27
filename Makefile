# Build and test Turttyl. Run `make help` for the list.
BIN      := build/turttyl
VERSION  := $(shell bash $(HOME)/claude-standards/plugin/company-standards/scripts/version.sh label 2>/dev/null || echo 0.1.0-alpha.dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

ifeq ($(shell uname),Darwin)
export MACOSX_DEPLOYMENT_TARGET := 13.0
export CGO_CFLAGS  := -mmacosx-version-min=13.0
export CGO_LDFLAGS := -mmacosx-version-min=13.0
endif

.PHONY: help frontend build run app test test-real lint vuln clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-10s %s\n", $$1, $$2}'

frontend: ## Build the React UI into frontend/dist
	cd frontend && npm ci --no-fund --no-audit && npm run build

build: frontend ## Build the app binary (build/turttyl)
	go build -tags production -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/turttyl

run: frontend ## Run the app in development mode (devtools enabled)
	go run ./cmd/turttyl

app: build ## macOS only: wrap the binary in "build/Turttyl.app"
	rm -rf "build/Turttyl.app"
	mkdir -p "build/Turttyl.app/Contents/MacOS"
	cp $(BIN) "build/Turttyl.app/Contents/MacOS/turttyl"
	sed 's/@VERSION@/$(VERSION)/g' packaging/macos/Info.plist > "build/Turttyl.app/Contents/Info.plist"

test: frontend ## Run all Go tests (race detector) and the UI type-check
	go test -race -count=1 ./...
	cd frontend && npm run typecheck

test-real: ## Also test against your own shell setup (not hermetic)
	AT_REAL_SHELL_TEST=1 go test -count=1 -run TestRealShell -v ./internal/adapters/ptyshell/

lint: ## go vet (macOS and Linux), staticcheck, gofmt
	go vet ./...
	GOOS=linux go vet ./internal/...
	staticcheck ./...
	@test -z "$$(gofmt -l cmd internal frontend/embed.go)" || (gofmt -l cmd internal; exit 1)

vuln: ## Vulnerability scans (Go and npm)
	govulncheck ./...
	cd frontend && npm audit

clean: ## Remove build output
	rm -rf build frontend/dist
