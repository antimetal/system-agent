// SPDX-License-Identifier: GPL-2.0-only
/* Anonymous Memory Ratio Test
 * 
 * This test creates high anonymous memory usage (heap allocations)
 * with minimal file-backed memory, triggering the anonymous ratio threshold.
 * 
 * Expected behavior:
 * - Anonymous memory >80% of total RSS
 * - File-backed memory stays low
 * - Triggers anonymous ratio detection
 * - Confidence score: 65-75
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <time.h>

#define HEAP_CHUNK_SIZE (5 * 1024 * 1024)   // 5MB heap chunks
#define INITIAL_HEAP_MB 100                  // Start with 100MB heap
#define HEAP_GROWTH_MB 10                    // Grow by 10MB per iteration
#define FILE_MAP_MB 20                       // Small file mapping for contrast

void print_memory_breakdown(const char *phase) {
    char status_path[256];
    snprintf(status_path, sizeof(status_path), "/proc/%d/status", getpid());
    
    FILE *f = fopen(status_path, "r");
    if (!f) return;
    
    char line[256];
    long vmrss = 0, rss_anon = 0, rss_file = 0, rss_shmem = 0;
    
    while (fgets(line, sizeof(line), f)) {
        if (sscanf(line, "VmRSS: %ld kB", &vmrss) == 1) continue;
        if (sscanf(line, "RssAnon: %ld kB", &rss_anon) == 1) continue;
        if (sscanf(line, "RssFile: %ld kB", &rss_file) == 1) continue;
        if (sscanf(line, "RssShmem: %ld kB", &rss_shmem) == 1) continue;
    }
    fclose(f);
    
    double anon_ratio = vmrss > 0 ? (double)rss_anon * 100 / vmrss : 0;
    
    printf("[%s]\n", phase);
    printf("  Total RSS: %ld MB\n", vmrss/1024);
    printf("  Anonymous: %ld MB (%.1f%%)\n", rss_anon/1024, anon_ratio);
    printf("  File-back: %ld MB (%.1f%%)\n", rss_file/1024, 
           vmrss > 0 ? (double)rss_file * 100 / vmrss : 0);
    printf("  Shared:    %ld MB\n", rss_shmem/1024);
    
    if (anon_ratio > 80) {
        printf("  *** ANONYMOUS RATIO THRESHOLD EXCEEDED (>80%%) ***\n");
    }
    printf("\n");
    fflush(stdout);
}

// Allocate and touch heap memory to ensure it's anonymous
void *allocate_heap(size_t size) {
    void *ptr = malloc(size);
    if (ptr) {
        // Touch all pages to ensure they're in RSS as anonymous
        memset(ptr, 0xAA, size);
    }
    return ptr;
}

// Create a file mapping (shows as file-backed memory)
void *create_file_mapping(size_t size) {
    // Create a temporary file
    char tmp_name[] = "/tmp/anon_test_XXXXXX";
    int fd = mkstemp(tmp_name);
    if (fd < 0) {
        perror("mkstemp");
        return NULL;
    }
    
    // Extend file to desired size
    if (ftruncate(fd, size) < 0) {
        perror("ftruncate");
        close(fd);
        unlink(tmp_name);
        return NULL;
    }
    
    // Map the file
    void *map = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (map == MAP_FAILED) {
        perror("mmap");
        close(fd);
        unlink(tmp_name);
        return NULL;
    }
    
    // Touch the mapping to bring it into RSS as file-backed
    memset(map, 0xBB, size);
    
    close(fd);
    unlink(tmp_name);  // File persists until unmapped
    
    return map;
}

int main(int argc, char *argv[]) {
    int duration_sec = 180;     // Default 3 minutes
    int target_anon_mb = 200;   // Target anonymous memory
    
    if (argc > 1) duration_sec = atoi(argv[1]);
    if (argc > 2) target_anon_mb = atoi(argv[2]);
    
    printf("=== Anonymous Memory Ratio Test ===\n");
    printf("Duration: %d seconds\n", duration_sec);
    printf("Target anonymous memory: %d MB\n", target_anon_mb);
    printf("Expected: Anonymous ratio >80%%\n\n");
    
    print_memory_breakdown("Initial");
    
    // Phase 1: Create some file-backed memory for contrast
    printf("Phase 1: Creating %d MB file-backed memory...\n", FILE_MAP_MB);
    void *file_map = create_file_mapping(FILE_MAP_MB * 1024 * 1024);
    if (!file_map) {
        printf("Warning: Could not create file mapping\n");
    }
    sleep(2);
    print_memory_breakdown("After file mapping");
    
    // Phase 2: Allocate initial heap (anonymous memory)
    printf("Phase 2: Allocating %d MB initial heap...\n", INITIAL_HEAP_MB);
    void **heap_chunks = malloc(1000 * sizeof(void*));
    int chunk_count = 0;
    
    size_t total_heap = 0;
    while (total_heap < INITIAL_HEAP_MB * 1024 * 1024) {
        void *chunk = allocate_heap(HEAP_CHUNK_SIZE);
        if (!chunk) break;
        heap_chunks[chunk_count++] = chunk;
        total_heap += HEAP_CHUNK_SIZE;
    }
    sleep(2);
    print_memory_breakdown("After initial heap");
    
    // Phase 3: Gradually increase heap to push anonymous ratio higher
    printf("Phase 3: Growing heap to exceed 80%% anonymous ratio...\n\n");
    
    time_t start_time = time(NULL);
    int growth_iterations = 0;
    
    while (time(NULL) - start_time < duration_sec) {
        // Add more heap memory
        for (int i = 0; i < 2; i++) {  // 10MB per iteration
            if (chunk_count >= 1000) break;
            void *chunk = allocate_heap(HEAP_CHUNK_SIZE);
            if (!chunk) break;
            heap_chunks[chunk_count++] = chunk;
            total_heap += HEAP_CHUNK_SIZE;
        }
        
        growth_iterations++;
        
        // Print stats every 10 iterations (30 seconds)
        if (growth_iterations % 10 == 0) {
            printf("Iteration %d - Total heap: %zu MB\n", 
                   growth_iterations, total_heap / (1024 * 1024));
            print_memory_breakdown("Growing");
        }
        
        // Touch some existing memory to keep it hot
        for (int i = 0; i < chunk_count && i < 10; i++) {
            memset(heap_chunks[i], growth_iterations & 0xFF, 4096);
        }
        
        sleep(3);
        
        // Stop if we've reached target
        if (total_heap >= target_anon_mb * 1024 * 1024) {
            printf("Target anonymous memory reached\n");
            break;
        }
    }
    
    print_memory_breakdown("Final");
    
    // Hold memory for observation
    printf("Holding memory for 30 seconds for observation...\n");
    sleep(30);
    
    // Cleanup
    printf("Cleaning up...\n");
    for (int i = 0; i < chunk_count; i++) {
        free(heap_chunks[i]);
    }
    free(heap_chunks);
    
    if (file_map) {
        munmap(file_map, FILE_MAP_MB * 1024 * 1024);
    }
    
    print_memory_breakdown("After cleanup");
    
    return 0;
}