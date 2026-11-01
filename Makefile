GO ?= go

.PHONY: all build vet test check ci fmt hooks clean

all: check

build:
	$(GO) build -o bin/dm ./cmd/dm

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./...

fmt:
	gofmt -w .

# Authoritative gate: gofmt + vet + race tests + build. Same command CI and the
# pre-commit hook run. Never claim green without this passing.
check:
	./scripts/check.sh

ci: check

# One-time per clone: point git at the repo's hooks.
hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit hook enabled (core.hooksPath=.githooks)"

clean:
	rm -rf bin
