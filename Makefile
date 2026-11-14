GO ?= go

.PHONY: all build desktop vet test check check-desktop ci fmt hooks clean

all: check

build:
	$(GO) build -o bin/dm ./cmd/dm

# GTK4 client: separate module, needs cgo plus the GTK4 dev headers.
desktop:
	cd desktop && $(GO) build -o ../bin/dm-desktop .

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

# GTK4 client gate: separate module, needs cgo plus the GTK4 dev headers.
check-desktop:
	./scripts/check-desktop.sh

ci: check

# One-time per clone: point git at the repo's hooks.
hooks:
	git config core.hooksPath .githooks
	@echo "pre-commit hook enabled (core.hooksPath=.githooks)"

clean:
	rm -rf bin
