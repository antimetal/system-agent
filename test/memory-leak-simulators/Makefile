# Makefile for memory leak simulator tests

CC = gcc
CFLAGS = -Wall -O2 -g
TARGETS = vsz_divergence monotonic_growth anon_ratio combined_leak cache_growth

.PHONY: all clean

all: $(TARGETS)

%: %.c
	$(CC) $(CFLAGS) -o $@ $<

# Build for x86_64 (for Hetzner)
build-x64:
	for target in $(TARGETS); do \
		x86_64-linux-gnu-gcc $(CFLAGS) -static -o $${target}_x64 $${target}.c; \
	done

# Build test package for deployment
package: build-x64
	tar czf memory-leak-tests.tar.gz *_x64 *.sh README.md

clean:
	rm -f $(TARGETS) *_x64 *.tar.gz

# Local test targets
test-vsz:
	./vsz_divergence 120 5

test-mono:
	./monotonic_growth 360 1024

test-anon:
	./anon_ratio 120 150

test-combined:
	./combined_leak 420

test-cache:
	./cache_growth 120

# Run all tests sequentially
test-all: all
	@echo "Running all memory leak tests..."
	@echo "================================"
	@./run-tests.sh