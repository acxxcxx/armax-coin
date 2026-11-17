.PHONY: all build clean run test test-race vet release

BIN_DIR=bin
BINARY_DAEMON=$(BIN_DIR)/armcoind
BINARY_MINER=$(BIN_DIR)/armminer
BINARY_SEND=$(BIN_DIR)/armsend
BINARY_WALLET=$(BIN_DIR)/armwallet
BINARY_GUI=$(BIN_DIR)/armgui

all: build

build:
	@echo "Building Armaxcoin binaries into $(BIN_DIR)/..."
	@mkdir -p $(BIN_DIR)
	go build -ldflags="-s -w" -o $(BINARY_DAEMON) .
	go build -ldflags="-s -w" -o $(BINARY_MINER) ./miner
	go build -ldflags="-s -w" -o $(BINARY_SEND) ./cli/armsend
	go build -ldflags="-s -w" -o $(BINARY_WALLET) ./cli/armwallet
	go build -ldflags="-s -w" -o $(BINARY_GUI) ./gui
	@echo "Binaries built successfully."

run:
	./$(BINARY_DAEMON) -port 19332 -p2p 19334

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

clean:
	rm -rf $(BIN_DIR) armcoind armminer armsend armwallet armgui *.exe blockchain.db.tmp
