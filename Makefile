.PHONY: all build clean run test release

BINARY_DAEMON=armcoind
BINARY_MINER=armminer
BINARY_SEND=armsend
BINARY_WALLET=armwallet
BINARY_GUI=armgui

all: build

build:
	@echo "Building Armaxcoin binaries..."
	go build -ldflags="-s -w" -o $(BINARY_DAEMON) .
	go build -ldflags="-s -w" -o $(BINARY_MINER) ./miner
	go build -ldflags="-s -w" -o $(BINARY_SEND) ./cli/armsend
	go build -ldflags="-s -w" -o $(BINARY_WALLET) ./cli/armwallet
	go build -ldflags="-s -w" -o $(BINARY_GUI) ./gui
	@echo "Binaries built successfully."

run:
	./$(BINARY_DAEMON) -port 19332 -p2p 19334

test:
	go run main.go -test

clean:
	rm -f $(BINARY_DAEMON) $(BINARY_MINER) $(BINARY_SEND) $(BINARY_WALLET) $(BINARY_GUI) *.exe blockchain.db.tmp
