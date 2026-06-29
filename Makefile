BINARY=monitor
CMD=./cmd/monitor
LDFLAGS=-s -w

.PHONY: all build amd64 arm clean

all: amd64 arm

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 $(CMD)

arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 $(CMD)

clean:
	rm -rf bin/
