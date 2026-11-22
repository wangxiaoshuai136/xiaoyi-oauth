PLUGIN_ID := xiaoyiwork
VERSION ?= 0.1.0
GO ?= go

.PHONY: test vet build hostsim clean

test:
	$(GO) test ./internal/...

vet:
	$(GO) vet ./internal/...

build:
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 GOAMD64=v1 \
		go build -buildmode=c-shared -trimpath -ldflags "-s -w" \
		-o bin/$(PLUGIN_ID)-v$(VERSION).dll ./cmd/xiaoyiwork-plugin

hostsim:
	CGO_ENABLED=0 go build -o bin/hostsim.exe ./cmd/hostsim

clean:
	rm -rf bin
