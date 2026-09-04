.PHONY: build install clean

BINARY_NAME=lav
INSTALL_DIR=$(HOME)/.local/bin

# Taken from the repository, so a build carries the commit it came from rather
# than a fixed number. Falls back to "dev" outside a checkout. Assigned once:
# with ?= the command would run again for every use, and a tree that turned
# dirty in between would stamp the binary and its directory differently.
VERSION := $(or $(VERSION),$(shell git describe --tags --always --dirty 2>/dev/null || echo dev))

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY_NAME) .

install: build
	# Create version directory
	mkdir -p $(HOME)/.local/share/lav/lav/$(VERSION)/bin
	cp $(BINARY_NAME) $(HOME)/.local/share/lav/lav/$(VERSION)/bin/
	# Create/update current symlink
	rm -f $(HOME)/.local/share/lav/lav/current
	ln -s $(VERSION) $(HOME)/.local/share/lav/lav/current
	# Create/update bin symlink
	mkdir -p $(HOME)/.local/bin
	rm -f $(HOME)/.local/bin/lav
	ln -s ../share/lav/lav/current/bin/lav $(HOME)/.local/bin/lav

clean:
	rm -f $(BINARY_NAME)
