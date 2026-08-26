# FanControl build orchestration.
#
# Builds the web SPA (Vite -> web/dist), then builds the Go binary with the
# dist embedded via go:embed. Cross-compiles to a static linux/amd64 binary for
# the rig. Run `make` (or `make build`) from the repo root.

BINARY     := fanctrl
LINUXBIN   := deploy/fanctrl-linux-amd64
GO         := go
NPM        := npm

.PHONY: all build web test vet lint clean linux distclean run

all: build

# Build the whole thing for linux/amd64 (the rig target) and place it in deploy/.
build: web linux

# Rebuild the embedded SPA into web/dist.
web:
	cd web && $(NPM) install
	cd web && $(NPM) run build

# Cross-compile the static Linux binary (embeds web/dist).
linux: web
	cd cmd/fanctrl
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "-s -w" -o $(LINUXBIN) ./cmd/fanctrl
	@echo "built $(LINUXBIN)"

# Build a native (host OS) binary for local testing.
native: web
	$(GO) build -o $(BINARY) ./cmd/fanctrl
	@echo "built ./$(BINARY)"

test:
	cd web && $(NPM) run build
	$(GO) test ./...

vet:
	$(GO) vet ./...

# Static checks + tests together.
check: vet test

run: native
	./$(BINARY) --provider mock --bind 127.0.0.1:8080

clean:
	rm -f $(BINARY) $(BINARY).exe $(LINUXBIN)
	rm -rf web/dist

distclean: clean
	cd web && rm -rf node_modules
