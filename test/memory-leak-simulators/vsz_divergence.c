// SPDX-License-Identifier: GPL-2.0-only
/* VSZ/RSS Divergence Test
 * 
 * This test allocates memory without touching it, causing VSZ to grow
 * much faster than RSS. This simulates the "allocation without use" pattern.
 * 
 * Expected behavior:
 * - VSZ grows rapidly (100MB/s)
 * - RSS stays relatively stable
 * - VSZ/RSS ratio exceeds 2.0 within 30 seconds
 * - Confidence score: 60-70
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/mman.h>
#include <time.h>

#define ALLOC_SIZE (10 * 1024 * 1024)  // 10MB per allocation
#define ALLOC_INTERVAL_MS 100           // 100ms = 100MB/s
#define TOUCH_PERCENTAGE 10              // Touch only 10% of allocated memory

void print_memory_stats(const char *phase) {
    char status_path[256];
    snprintf(status_path, sizeof(status_path), "/proc/%d/status", getpid());
    
    FILE *f = fopen(status_path, "r");
    if (!f) return;
    
    char line[256];
    long vmsize = 0, vmrss = 0;
    
    while (fgets(line, sizeof(line), f)) {
        if (sscanf(line, "VmSize: %ld kB", &vmsize) == 1) continue;
        if (sscanf(line, "VmRSS: %ld kB", &vmrss) == 1) continue;
    }
    fclose(f);
    
    double ratio = vmrss > 0 ? (double)vmsize / vmrss : 0;
    
    printf("[%s] VSZ: %ld MB, RSS: %ld MB, Ratio: %.2f\n", 
           phase, vmsize/1024, vmrss/1024, ratio);
    fflush(stdout);
}

int main(int argc, char *argv[]) {
    int duration_sec = 120;  // Default 2 minutes
    int touch_percent = TOUCH_PERCENTAGE;
    
    if (argc > 1) duration_sec = atoi(argv[1]);
    if (argc > 2) touch_percent = atoi(argv[2]);
    
    printf("=== VSZ/RSS Divergence Test ===\n");
    printf("Duration: %d seconds\n", duration_sec);
    printf("Touch percentage: %d%%\n", touch_percent);
    printf("Allocation rate: 100MB/s (10MB every 100ms)\n");
    printf("Expected: VSZ/RSS ratio > 2.0\n\n");
    
    print_memory_stats("Initial");
    
    time_t start_time = time(NULL);
    int iteration = 0;
    void **allocations = malloc(10000 * sizeof(void*));
    int alloc_count = 0;
    
    while (time(NULL) - start_time < duration_sec) {
        // Allocate memory using malloc (goes to VSZ immediately)
        void *ptr = malloc(ALLOC_SIZE);
        if (!ptr) {
            printf("Allocation failed at iteration %d\n", iteration);
            break;
        }
        
        // Store pointer to prevent optimization
        allocations[alloc_count++] = ptr;
        
        // Touch only a small percentage to keep RSS low
        // This simulates allocated but unused memory
        size_t touch_size = (ALLOC_SIZE * touch_percent) / 100;
        if (touch_size > 0) {
            // Touch first part of allocation
            memset(ptr, 0x42, touch_size);
        }
        
        iteration++;
        
        // Print stats every 10 iterations (every second)
        if (iteration % 10 == 0) {
            print_memory_stats("Running");
        }
        
        // Sleep to control allocation rate
        usleep(ALLOC_INTERVAL_MS * 1000);
        
        // Safety check
        if (alloc_count >= 10000) {
            printf("Maximum allocations reached\n");
            break;
        }
    }
    
    print_memory_stats("Final");
    
    // Hold memory for observation
    printf("\nHolding memory for 30 seconds for observation...\n");
    sleep(30);
    
    // Cleanup
    for (int i = 0; i < alloc_count; i++) {
        free(allocations[i]);
    }
    free(allocations);
    
    print_memory_stats("After cleanup");
    
    return 0;
}