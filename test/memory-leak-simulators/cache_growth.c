// SPDX-License-Identifier: GPL-2.0-only
/* Cache Growth Test (False Positive Test)
 * 
 * This test simulates legitimate cache growth using file mappings.
 * This should NOT trigger leak detection.
 * 
 * Expected behavior:
 * - File-backed memory grows (not anonymous)
 * - VSZ/RSS ratio stays normal
 * - No monotonic pattern (cache eviction)
 * - Confidence score: <20
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <time.h>

#define CACHE_FILE_SIZE (10 * 1024 * 1024)  // 10MB files
#define MAX_CACHE_FILES 20

void print_memory_stats(const char *phase) {
    char status_path[256];
    snprintf(status_path, sizeof(status_path), "/proc/%d/status", getpid());
    
    FILE *f = fopen(status_path, "r");
    if (!f) return;
    
    char line[256];
    long vmsize = 0, vmrss = 0, rss_anon = 0, rss_file = 0;
    
    while (fgets(line, sizeof(line), f)) {
        if (sscanf(line, "VmSize: %ld kB", &vmsize) == 1) continue;
        if (sscanf(line, "VmRSS: %ld kB", &vmrss) == 1) continue;
        if (sscanf(line, "RssAnon: %ld kB", &rss_anon) == 1) continue;
        if (sscanf(line, "RssFile: %ld kB", &rss_file) == 1) continue;
    }
    fclose(f);
    
    double vsz_rss_ratio = vmrss > 0 ? (double)vmsize / vmrss : 0;
    double anon_ratio = vmrss > 0 ? (double)rss_anon * 100 / vmrss : 0;
    double file_ratio = vmrss > 0 ? (double)rss_file * 100 / vmrss : 0;
    
    printf("[%s]\n", phase);
    printf("  VSZ/RSS: %.2f (should stay <1.5)\n", vsz_rss_ratio);
    printf("  Anonymous: %.1f%% (should stay low)\n", anon_ratio);
    printf("  File-backed: %.1f%% (should be high)\n", file_ratio);
    printf("  RSS: %ld MB (File: %ld MB, Anon: %ld MB)\n", 
           vmrss/1024, rss_file/1024, rss_anon/1024);
    printf("\n");
    fflush(stdout);
}

struct cache_entry {
    int fd;
    void *map;
    size_t size;
    char filename[256];
};

int main(int argc, char *argv[]) {
    int duration_sec = 180;  // 3 minutes
    
    if (argc > 1) duration_sec = atoi(argv[1]);
    
    printf("=== Cache Growth Test (False Positive) ===\n");
    printf("Duration: %d seconds\n", duration_sec);
    printf("Expected: File-backed growth, NO leak detection\n");
    printf("Target confidence: <20\n\n");
    
    print_memory_stats("Initial");
    
    struct cache_entry cache[MAX_CACHE_FILES];
    int cache_count = 0;
    
    // Create temp files for caching
    printf("Creating cache files...\n");
    for (int i = 0; i < MAX_CACHE_FILES; i++) {
        snprintf(cache[i].filename, sizeof(cache[i].filename), 
                 "/tmp/cache_test_%d.dat", i);
        
        cache[i].fd = open(cache[i].filename, O_CREAT | O_RDWR, 0600);
        if (cache[i].fd < 0) {
            perror("open");
            continue;
        }
        
        // Write data to file
        if (ftruncate(cache[i].fd, CACHE_FILE_SIZE) < 0) {
            perror("ftruncate");
            close(cache[i].fd);
            continue;
        }
        
        // Initially don't map - simulate cache warming
        cache[i].map = NULL;
        cache[i].size = CACHE_FILE_SIZE;
        cache_count++;
    }
    
    printf("Created %d cache files\n\n", cache_count);
    
    time_t start_time = time(NULL);
    int iteration = 0;
    
    printf("Simulating cache access patterns...\n\n");
    
    while (time(NULL) - start_time < duration_sec) {
        iteration++;
        
        // Simulate cache warming - gradually map files
        if (iteration <= cache_count) {
            int idx = iteration - 1;
            printf("Loading cache file %d into memory...\n", idx);
            
            cache[idx].map = mmap(NULL, cache[idx].size, 
                                 PROT_READ | PROT_WRITE,
                                 MAP_SHARED, cache[idx].fd, 0);
            
            if (cache[idx].map != MAP_FAILED) {
                // Touch the mapping to load it into cache
                memset(cache[idx].map, iteration & 0xFF, cache[idx].size);
                msync(cache[idx].map, cache[idx].size, MS_SYNC);
            }
        }
        
        // Simulate cache eviction and reload (non-monotonic pattern)
        if (iteration > 10 && (iteration % 5) == 0) {
            int evict_idx = rand() % cache_count;
            
            if (cache[evict_idx].map && cache[evict_idx].map != MAP_FAILED) {
                printf("Evicting cache entry %d (simulating pressure)...\n", evict_idx);
                munmap(cache[evict_idx].map, cache[evict_idx].size);
                cache[evict_idx].map = NULL;
                
                // This causes RSS to drop (non-monotonic)
                sleep(1);
                
                // Remap it (cache miss -> reload)
                cache[evict_idx].map = mmap(NULL, cache[evict_idx].size,
                                           PROT_READ | PROT_WRITE,
                                           MAP_SHARED, cache[evict_idx].fd, 0);
                if (cache[evict_idx].map != MAP_FAILED) {
                    // Touch first page only (lazy loading)
                    volatile char *p = cache[evict_idx].map;
                    *p = iteration & 0xFF;
                }
            }
        }
        
        // Print stats every 20 seconds
        if ((iteration % 20) == 0) {
            print_memory_stats("Cache active");
        }
        
        sleep(1);
    }
    
    print_memory_stats("Final");
    
    printf("Confidence check:\n");
    printf("- VSZ/RSS should be <1.5 (no divergence)\n");
    printf("- Anonymous ratio should be <40%% (mostly file-backed)\n");
    printf("- Growth pattern non-monotonic (evictions)\n");
    printf("Expected confidence: <20\n\n");
    
    // Cleanup
    printf("Cleaning up cache files...\n");
    for (int i = 0; i < cache_count; i++) {
        if (cache[i].map && cache[i].map != MAP_FAILED) {
            munmap(cache[i].map, cache[i].size);
        }
        if (cache[i].fd >= 0) {
            close(cache[i].fd);
        }
        unlink(cache[i].filename);
    }
    
    print_memory_stats("After cleanup");
    
    return 0;
}