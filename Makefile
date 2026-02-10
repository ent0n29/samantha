APP_NAME := samantha

.PHONY: build test run fmt dev docker-build docker-run setup-local-voice

# Prefer an arm64-native Go toolchain on Apple Silicon, even if the current shell is x86_64.
ARM64_GO := $(HOME)/.local/arm64/go/bin/go
ifeq ($(wildcard $(ARM64_GO)),)
GO := go
else
GO := arch -arm64 $(ARM64_GO)
endif

build:
	mkdir -p bin
	$(GO) build -o bin/$(APP_NAME) ./cmd/samantha

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/samantha

fmt:
	$(GO) fmt ./...

lint: fmt

dev:
	./scripts/dev

setup-local-voice:
	./scripts/setup_local_voice.sh

docker-build:
	docker build -t $(APP_NAME):latest .

docker-run:
	docker run --rm -p 8080:8080 --env-file .env $(APP_NAME):latest
