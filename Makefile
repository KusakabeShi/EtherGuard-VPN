PREFIX ?= /usr
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin
export GO111MODULE := on

all: generate-version-and-build

MAKEFLAGS += --no-print-directory

generate-version-and-build:
	@export GIT_CEILING_DIRECTORIES="$(realpath $(CURDIR)/..)" && \
	tag="$$(git describe 2>/dev/null)" && \
	ver="$$(printf 'package main\n\nconst Version = "%s"\n' "$$tag")" && \
	[ "$$(cat version.go 2>/dev/null)" != "$$ver" ] && \
	echo "$$ver" > version.go && \
	git update-index --assume-unchanged version.go || true
	@$(MAKE) etherguard-go

etherguard-go: $(wildcard *.go) $(wildcard */*.go)
	go mod download && \
	go mod tidy && \
	go mod vendor && \
	go build -v -o "$@"

etherguard-go-static: $(wildcard *.go) $(wildcard */*.go)
	go mod download && \
	go mod tidy && \
	go mod vendor && \
	CGO_ENABLED=0 go build -a -trimpath -ldflags '-s -w -extldflags "-static"'  -v -o "$@"

vpp:
	@export GIT_CEILING_DIRECTORIES="$(realpath $(CURDIR)/..)" && \
	tag="$$(git describe 2>/dev/null)" && \
	ver="$$(printf 'package main\n\nconst Version = "%s"\n' "$$tag")" && \
	[ "$$(cat version.go 2>/dev/null)" != "$$ver" ] && \
	echo "$$ver" > version.go && \
	git update-index --assume-unchanged version.go || true
	@$(MAKE) etherguard-go-vpp

etherguard-go-vpp: export CGO_CFLAGS ?= -I/usr/include/memif
etherguard-go-vpp: $(wildcard *.go) $(wildcard */*.go)
	go mod download && \
	go mod tidy && \
	go mod vendor && \
	patch -p0 -i govpp_remove_crcstring_check.patch && \
	go build -v -tags vpp -o "$@"

static:
	@export GIT_CEILING_DIRECTORIES="$(realpath $(CURDIR)/..)" && \
	tag="$$(git describe 2>/dev/null)" && \
	ver="$$(printf 'package main\n\nconst Version = "%s"\n' "$$tag")" && \
	[ "$$(cat version.go 2>/dev/null)" != "$$ver" ] && \
	echo "$$ver" > version.go && \
	git update-index --assume-unchanged version.go || true
	@$(MAKE) etherguard-go-static

install: etherguard-go
	@install -v -d "$(DESTDIR)$(BINDIR)" && install -v -m 0755 "$<" "$(DESTDIR)$(BINDIR)/etherguard-go"

test:
	go test -v ./...

clean:
	rm -f etherguard-go
	rm -f etherguard-go-static
	rm -f etherguard-go-vpp
	rm -f etherguard-go-vpp-static

.PHONY: all clean test install generate-version-and-build
