# Developer tasks; fork suites remain separate Go modules and are checked explicitly.
.PHONY: build test vet bench-build

# Build the CLI from this checkout; dirty runtime edits are included.
build:
	go build -trimpath -o build/super-paqet ./cmd

# Check root races and both fork suites; namespace/WAN qualification is separate.
test:
	go test -race ./...
	cd third_party/smux && go test -race -timeout 10m ./...
	cd third_party/kcp-go && go test -timeout 15m ./...

# Check every module boundary, including the local transport replacements.
vet:
	go vet ./...
	cd third_party/smux && go vet ./...
	cd third_party/kcp-go && go vet ./...

# Build the isolated workload/target utility, not the deployed service.
bench-build:
	go build -o build/spq-bench ./cmd/bench
