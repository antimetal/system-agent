// SPDX-License-Identifier: GPL-2.0-only
/* Combined Leak Test
 * 
 * This test triggers all three detection thresholds simultaneously
 * to achieve maximum confidence score.
 * 
 * Expected behavior:
 * - VSZ/RSS ratio > 2.0 (allocation without use)
 * - Monotonic growth > 5 minutes
 * - Anonymous ratio > 80%
 * - Confidence score: 95-100
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>
#include <signal.h>

volatile int keep_running = 1;

void signal_handler(int sig) {
    keep_running = 0;
}

void print_full_stats(const char *phase, time_t start_time) {
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
    
    int elapsed = time(NULL) - start_time;
    double vsz_rss_ratio = vmrss > 0 ? (double)vmsize / vmrss : 0;
    double anon_ratio = vmrss > 0 ? (double)rss_anon * 100 / vmrss : 0;
    
    printf("\n[%3ds] === %s ===\n", elapsed, phase);
    printf("  VSZ: %ld MB, RSS: %ld MB\n", vmsize/1024, vmrss/1024);
    printf("  VSZ/RSS Ratio: %.2f %s\n", vsz_rss_ratio,
           vsz_rss_ratio > 2.0 ? "[THRESHOLD MET]" : "");
    printf("  Anonymous: %ld MB (%.1f%%) %s\n", rss_anon/1024, anon_ratio,
           anon_ratio > 80 ? "[THRESHOLD MET]" : "");
    printf("  File-backed: %ld MB\n", rss_file/1024);
    
    if (elapsed > 300) {
        printf("  Monotonic growth: %d seconds [THRESHOLD MET]\n", elapsed);
    } else {
        printf("  Monotonic growth: %d seconds (need 300s)\n", elapsed);
    }
    
    // Estimate confidence score
    int confidence = 0;
    if (vsz_rss_ratio > 2.0) confidence += 30;
    if (elapsed > 300) confidence += 35;
    if (anon_ratio > 80) confidence += 35;
    
    printf("  Estimated confidence: %d/100\n", confidence);
    printf("\n");
    fflush(stdout);
}

int main(int argc, char *argv[]) {
    int duration_sec = 420;  // 7 minutes to ensure monotonic threshold
    
    if (argc > 1) duration_sec = atoi(argv[1]);
    
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);
    
    printf("=== Combined Leak Test (All Thresholds) ===\n");
    printf("Duration: %d seconds\n", duration_sec);
    printf("Expected: All three thresholds triggered\n");
    printf("Target confidence: >95\n\n");
    
    time_t start_time = time(NULL);
    print_full_stats("Initial", start_time);
    
    // Storage for our allocations
    void **untouched_allocs = malloc(10000 * sizeof(void*));
    void **touched_allocs = malloc(10000 * sizeof(void*));
    int untouched_count = 0;
    int touched_count = 0;
    
    printf("Starting combined leak pattern...\n");
    printf("- Allocating without touching (VSZ divergence)\n");
    printf("- Steady heap growth (monotonic + anonymous)\n\n");
    
    time_t last_print = time(NULL);
    time_t five_min_mark_printed = 0;
    
    while (keep_running && (time(NULL) - start_time < duration_sec)) {
        // Component 1: VSZ divergence - allocate but don't touch
        void *untouched = malloc(5 * 1024 * 1024);  // 5MB
        if (untouched && untouched_count < 10000) {
            // Don't touch this memory (VSZ grows, RSS doesn't)
            untouched_allocs[untouched_count++] = untouched;
        }
        
        // Component 2: Anonymous growth - allocate and touch heap
        void *touched = malloc(2 * 1024 * 1024);  // 2MB
        if (touched && touched_count < 10000) {
            // Touch all of it (RSS grows as anonymous)
            memset(touched, 0x42, 2 * 1024 * 1024);
            touched_allocs[touched_count++] = touched;
        }
        
        // Component 3: Ensure monotonic - never free anything
        // (Memory only grows, never shrinks)
        
        time_t now = time(NULL);
        
        // Print stats every 30 seconds
        if (now - last_print >= 30) {
            print_full_stats("Running", start_time);
            last_print = now;
        }
        
        // Special notification at 5 minutes
        if (five_min_mark_printed == 0 && now - start_time >= 300) {
            printf("*** 5-MINUTE MARK REACHED ***\n");
            printf("*** ALL THRESHOLDS SHOULD BE MET ***\n");
            print_full_stats("5-Minute Mark", start_time);
            five_min_mark_printed = 1;
        }
        
        // Sleep 200ms between iterations
        // This gives us: 5MB/200ms = 25MB/s VSZ growth
        //                2MB/200ms = 10MB/s RSS growth
        usleep(200000);
    }
    
    print_full_stats("Final", start_time);
    
    printf("Summary:\n");
    printf("- Untouched allocations: %d (%.1f MB VSZ only)\n", 
           untouched_count, untouched_count * 5.0);
    printf("- Touched allocations: %d (%.1f MB RSS)\n",
           touched_count, touched_count * 2.0);
    
    // Hold for observation
    printf("\nHolding memory for 30 seconds for observation...\n");
    sleep(30);
    
    // Cleanup
    printf("Cleaning up...\n");
    for (int i = 0; i < untouched_count; i++) {
        free(untouched_allocs[i]);
    }
    for (int i = 0; i < touched_count; i++) {
        free(touched_allocs[i]);
    }
    free(untouched_allocs);
    free(touched_allocs);
    
    print_full_stats("After cleanup", start_time);
    
    return 0;
}