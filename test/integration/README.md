# Integration & End-to-End Tests

This directory is reserved for cross-package integration and multi-node end-to-end tests.

## Go Package Testing Convention
- **Unit & package tests (`*_test.go`)** intentionally remain **co-located** inside their respective Go packages (`core/`, `node/`, `miner/`, `wallet/`, `cli/`, `gui/`, `armaxhash/`) so `go test ./...`, `-race`, coverage reports, and unexported helper tests work seamlessly with the standard Go toolchain.
- **Repository hygiene & root checks** remain at root (`hygiene_test.go`, `main_test.go`).
- **Cross-package E2E / multi-node harnesses** belong here under `test/integration/`.

## Running Tests
```bash
go test ./...
go test -race ./...
go vet -tags testhooks ./...
```
