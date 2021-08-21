PREFIX ?= /usr
DESTDIR ?=
BINDIR ?= $(PREFIX)/bin
export GO111MODULE := on

all: generate-version-and-build

MAKEFLAGS += --no-print-directory

generate-version-and-build:
	go mod download && \
	go mod tidy && \
	@export GIT_CEILING_DIRECTORIES="$(realpath $(CURDIR)/..)" && \
	tag="$$(git describe --dirty 2>/dev/null)" && \
	ver="$$(printf 'package main\n\nconst Version = "%s"\n' "$$tag")" && \
	[ "$$(cat version.go 2>/dev/null)" != "$$ver" ] && \
	echo "$$ver" > version.go && \
	git update-index --assume-unchanged version.go || true
	@$(MAKE) etherguard-go

etherguard-go: $(wildcard *.go) $(wildcard */*.go)
	go build -v -o "$@"

install: etherguard-go
	@install -v -d "$(DESTDIR)$(BINDIR)" && install -v -m 0755 "$<" "$(DESTDIR)$(BINDIR)/etherguard-go"

test:
	go test -v ./...

clean:
	rm -f etherguard-go

.PHONY: all clean test install generate-version-and-build
