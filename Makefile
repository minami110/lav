.PHONY: build install clean

BINARY_NAME=lav
INSTALL_DIR=$(HOME)/.local/bin
VERSION=0.0.0

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
