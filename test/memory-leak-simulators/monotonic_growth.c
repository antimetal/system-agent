// SPDX-License-Identifier: GPL-2.0-only
/* Monotonic Growth Test
 * 
 * This test creates a steady memory leak with consistent growth rate
 * and no decreases, triggering the monotonic growth threshold.
 * 
 * Expected behavior:
 * - RSS grows continuously without drops
 * - Growth persists for >5 minutes
 * - Triggers monotonic growth detection
 * - Confidence score: 70-80
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>
#include <signal.h>

#define LEAK_SIZE (256 * 1024)      // 256KB per leak
#define LEAK_INTERVAL_MS 250        // 4 leaks/second = 1MB/s
#define WARMUP_PERIOD_SEC 30        // Initial warmup before leak

volatile int keep_running = 1;

void signal_handler(int sig) {
    keep_running = 0;
    printf("\nReceived signal %d, stopping...\n", sig);
}

void print_memory_stats(const char *phase, time_t start_time) {
    char status_path[256];
    snprintf(status_path, sizeof(status_path), "/proc/%d/status", getpid());
    
    FILE *f = fopen(status_path, "r");
    if (!f) return;
    
    char line[256];
    long vmrss = 0, vmanon = 0;
    
    while (fgets(line, sizeof(line), f)) {
        if (sscanf(line, "VmRSS: %ld kB", &vmrss) == 1) continue;
        if (sscanf(line, "RssAnon: %ld kB", &vmanon) == 1) continue;
    }
    fclose(f);
    
    int elapsed = time(NULL) - start_time;
    printf("[%3ds] %s - RSS: %ld MB, Anon: %ld MB\n", 
           elapsed, phase, vmrss/1024, vmanon/1024);
    fflush(stdout);
}

// Linked list node for creating real heap fragmentation
struct leak_node {
    struct leak_node *next;
    char data[LEAK_SIZE - sizeof(struct leak_node*)];
};

int main(int argc, char *argv[]) {
    int duration_sec = 420;  // Default 7 minutes (>5 min threshold)
    int leak_rate_kb = 1024; // Default 1MB/s
    
    if (argc > 1) duration_sec = atoi(argv[1]);
    if (argc > 2) leak_rate_kb = atoi(argv[2]);
    
    // Setup signal handler
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);
    
    printf("=== Monotonic Growth Test ===\n");
    printf("Duration: %d seconds\n", duration_sec);
    printf("Leak rate: %d KB/s\n", leak_rate_kb);
    printf("Expected: Monotonic growth for >5 minutes\n\n");
    
    time_t start_time = time(NULL);
    print_memory_stats("Initial", start_time);
    
    // Warmup phase - allocate some initial memory
    printf("\nWarmup phase (%d seconds)...\n", WARMUP_PERIOD_SEC);
    void *warmup_mem = malloc(50 * 1024 * 1024);  // 50MB warmup
    if (warmup_mem) {
        memset(warmup_mem, 0x42, 50 * 1024 * 1024);  // Touch it all
    }
    sleep(WARMUP_PERIOD_SEC);
    print_memory_stats("After warmup", start_time);
    
    printf("\nStarting monotonic leak phase...\n");
    printf("Growth should trigger detection after 5 minutes\n\n");
    
    // Calculate leak interval based on desired rate
    int leak_size = LEAK_SIZE;
    int leaks_per_second = (leak_rate_kb * 1024) / leak_size;
    int interval_us = 1000000 / leaks_per_second;
    
    struct leak_node *leak_list = NULL;
    int leak_count = 0;
    time_t last_print = time(NULL);
    time_t phase_start = time(NULL);
    
    while (keep_running && (time(NULL) - start_time < duration_sec)) {
        // Create a new leak node
        struct leak_node *node = malloc(sizeof(struct leak_node));
        if (!node) {
            printf("Allocation failed after %d leaks\n", leak_count);
            break;
        }
        
        // Fill with data to ensure RSS growth
        memset(node->data, (leak_count & 0xFF), sizeof(node->data));
        
        // Add to linked list (prevents optimization and creates fragmentation)
        node->next = leak_list;
        leak_list = node;
        leak_count++;
        
        // Print stats every 30 seconds
        time_t now = time(NULL);
        if (now - last_print >= 30) {
            int phase_elapsed = now - phase_start;
            printf("Leak phase: %d seconds", phase_elapsed);
            if (phase_elapsed > 300) {
                printf(" [MONOTONIC THRESHOLD PASSED]");
            }
            printf("\n");
            print_memory_stats("Leaking", start_time);
            last_print = now;
        }
        
        // Important: Check for 5-minute mark
        if (now - phase_start == 300) {
            printf("\n*** 5-MINUTE MONOTONIC GROWTH ACHIEVED ***\n");
            printf("*** LEAK DETECTION SHOULD TRIGGER NOW ***\n\n");
        }
        
        usleep(interval_us);
    }
    
    print_memory_stats("Final", start_time);
    printf("\nTotal leaks: %d (%.1f MB)\n", 
           leak_count, (leak_count * LEAK_SIZE) / (1024.0 * 1024.0));
    
    // Hold memory for observation
    printf("\nHolding memory for 30 seconds for observation...\n");
    sleep(30);
    
    // Cleanup (in practice, leaks wouldn't be cleaned)
    printf("Cleaning up (this wouldn't happen in a real leak)...\n");
    while (leak_list) {
        struct leak_node *next = leak_list->next;
        free(leak_list);
        leak_list = next;
    }
    free(warmup_mem);
    
    print_memory_stats("After cleanup", start_time);
    
    return 0;
}