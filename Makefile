GOCACHE_DIR := $(CURDIR)/.cache/go-build
GO_BENCH_ENV := mkdir -p $(GOCACHE_DIR) .bench && env GOCACHE=$(GOCACHE_DIR)

.PHONY: build build-resolver test-unit test-integration test-e2e test bench bench-resolver bench-split bench-escape bench-profile clean

build:
	mkdir -p bin
	go build -o bin/clerm ./cmd/clerm

build-resolver:
	mkdir -p bin
	go build -o bin/clerm-resolver ./cmd/clerm-resolver

test-unit:
	go test ./internal/schema ./internal/clermcfg ./internal/clermreq ./internal/clermresp -count=1

test-integration:
	go test ./internal/resolver ./internal/app/resolvercli -count=1

test-e2e:
	go test ./e2e -count=1

bench:
	$(GO_BENCH_ENV) go test ./internal/clermcfg ./internal/clermreq ./internal/resolver -bench . -benchmem -run '^$$'

bench-resolver:
	$(GO_BENCH_ENV) go test ./internal/resolver -bench . -benchmem -run '^$$'

bench-split:
	$(GO_BENCH_ENV) go test ./internal/clermcfg ./internal/clermreq -bench '^(BenchmarkDecodeCLERMCFG|BenchmarkDecodeCLERMCFGCodecOnly|BenchmarkValidateCLERMCFGSemantics|BenchmarkRoundTripCLERMCFG|BenchmarkRoundTripCLERMCFGCodecOnly|BenchmarkDecodeCLERMRequest|BenchmarkDecodeCLERMRequestCodecOnly|BenchmarkValidateCLERMRequestSemantics|BenchmarkRoundTripCLERMRequest|BenchmarkRoundTripCLERMRequestCodecOnly)(/.*)?$$' -benchmem -run '^$$'

bench-escape:
	$(GO_BENCH_ENV) go test ./internal/clermcfg -run '^$$' -gcflags=all=-m=2 > /dev/null 2> .bench/clermcfg.escape.txt
	$(GO_BENCH_ENV) go test ./internal/clermreq -run '^$$' -gcflags=all=-m=2 > /dev/null 2> .bench/clermreq.escape.txt

bench-profile:
	$(GO_BENCH_ENV) go test ./internal/clermcfg -bench . -benchmem -run '^$$' -count=1 -cpuprofile .bench/clermcfg.cpu.pprof -memprofile .bench/clermcfg.mem.pprof
	$(GO_BENCH_ENV) go test ./internal/clermreq -bench . -benchmem -run '^$$' -count=1 -cpuprofile .bench/clermreq.cpu.pprof -memprofile .bench/clermreq.mem.pprof

test:
	go test ./... -count=1

clean:
	rm -rf bin dist .bench .cache schemas *.clerm *.clermcfg *.token *.tokens *.ed25519 *.ed25519.pub *.test *.out
