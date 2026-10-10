# cellar has two builds (see docs/design.md):
#   make cli   static nogui binary, on the host (pure Go, no cgo)
#   make gui   GUI binary, in a podman container holding the GTK/WebKit -dev
#              packages, so the host needs none of them.

IMAGE   ?= cellar-build
GOROOT  := $(shell go env GOROOT)
MODCACHE:= $(shell go env GOMODCACHE)
VERSION ?= $(or $(shell git describe --tags --always --dirty 2>/dev/null),dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# --user keeps target files owned by us (rootful podman). The host Go
# toolchain is static, so mounting it works on any base; the module cache is
# shared, the build cache is the container's own (different cgo flags).
PODMAN_RUN = podman run --rm --user "$$(id -u):$$(id -g)" \
	-v "$(CURDIR)":/work -w /work \
	-v "$(GOROOT)":/opt/go:ro \
	-v "$(MODCACHE)":/go/pkg/mod \
	-v "$(CURDIR)/.cache/go-build":/go/cache \
	-e GOMODCACHE=/go/pkg/mod -e GOCACHE=/go/cache -e HOME=/tmp \
	$(IMAGE)

.PHONY: cli gui image test clean flatpak swap

FLATPAK_ID := org.flatpark.Cellar
MANIFEST   := packaging/flatpak/$(FLATPAK_ID).yml

# Build and (re)install the local Flatpak.
flatpak: gui
	flatpak run org.flatpak.Builder --force-clean --disable-rofiles-fuse --repo=.cache/repo .cache/flatpak-build $(MANIFEST)
	flatpak build-bundle --runtime-repo=https://dl.flathub.org/repo/flathub.flatpakrepo .cache/repo .cache/cellar.flatpak $(FLATPAK_ID)
	flatpak install --user -y --noninteractive --reinstall --bundle .cache/cellar.flatpak

# Fast loop: drop a fresh dist/cellar into the installed Flatpak in place.
# --remove-destination unlinks first, so the OSTree object the deployed file
# is hard-linked to is left alone. Close the GUI, then run it again.
swap: gui
	cp --remove-destination dist/cellar "$$(flatpak info --user -l $(FLATPAK_ID))/files/bin/cellar"
	@echo "swapped; restart: flatpak kill $(FLATPAK_ID); flatpak run $(FLATPAK_ID)"

cli:
	CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o dist/cellar-cli .

image:
	podman build -t $(IMAGE) -f scripts/Containerfile scripts

gui: | .cache/go-build
	$(PODMAN_RUN) go build -tags production -trimpath -ldflags="$(LDFLAGS)" -o dist/cellar .

.cache/go-build:
	mkdir -p $@

test:
	go vet -tags nogui ./... && go test -tags nogui ./...

clean:
	rm -rf dist .cache
