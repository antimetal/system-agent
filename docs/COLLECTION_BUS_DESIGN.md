# Collection Bus Design Notes

## Problem Statement

Currently, performance collectors are being used by two separate managers:
- **Performance Manager**: Streams metrics for monitoring (seconds/milliseconds)
- **Hardware Manager**: Builds hardware graph topology (5 minutes)

Both managers trigger the same collectors independently, leading to:
- Duplicate file reads from `/proc` and `/sys`
- No coordination of collection timing
- Potential race conditions on shared resources
- Wasted CPU cycles

## Proposed Solution: Shared Collection Bus

### Core Concept

A single collection coordinator that:
1. Owns all performance collectors
2. Runs collection on a unified schedule
3. Publishes snapshots to multiple consumers
4. Caches results for deduplication

### Architecture

```
Performance Collectors
         │
         ▼
  Collection Bus (coordinator)
         │
    ┌────┴────┐
    ▼         ▼
Hardware   Metrics
Manager    Manager
```

### Design Sketch

```go
type CollectionBus struct {
    collectors map[MetricType]Collector
    
    // Different intervals for different data
    schedules map[MetricType]time.Duration
    
    // Subscribers by data type
    subscribers map[MetricType][]chan<- interface{}
    
    // Cache for deduplication
    cache map[MetricType]CachedResult
}

type CachedResult struct {
    Data      interface{}
    Timestamp time.Time
    TTL       time.Duration
}

// Subscription model
func (bus *CollectionBus) Subscribe(metricType MetricType, interval time.Duration) <-chan interface{} {
    ch := make(chan interface{}, 1)
    bus.subscribers[metricType] = append(bus.subscribers[metricType], ch)
    
    // Adjust collection schedule if needed
    if interval < bus.schedules[metricType] {
        bus.schedules[metricType] = interval
    }
    
    return ch
}
```

### Collection Strategy

#### Option 1: Unified Snapshots
```go
type Snapshot struct {
    Timestamp   time.Time
    CPUInfo     *CPUInfo      // Changes rarely (hardware)
    MemoryInfo  *MemoryInfo   // Changes rarely (hardware)
    CPUStats    *CPUStats     // Changes frequently (metrics)
    MemoryStats *MemoryStats  // Changes frequently (metrics)
}

// Problem: Forces everything to same interval
```

#### Option 2: Tiered Collection (Recommended)
```go
// Tier 1: Hardware (5 minutes) - Immutable
tier1 := []MetricType{
    MetricTypeCPUInfo,
    MetricTypeDiskInfo,
    MetricTypeNetworkInfo,
}

// Tier 2: System Stats (10 seconds) - Slow changing
tier2 := []MetricType{
    MetricTypeMemoryStats,
    MetricTypeLoadAvg,
}

// Tier 3: Process Stats (1 second) - Fast changing
tier3 := []MetricType{
    MetricTypeCPUStats,
    MetricTypeProcessStats,
}
```

### Caching Strategy

```go
func (bus *CollectionBus) Collect(metricType MetricType) (interface{}, error) {
    // Check cache first
    if cached, ok := bus.cache[metricType]; ok {
        if time.Since(cached.Timestamp) < cached.TTL {
            return cached.Data, nil
        }
    }
    
    // Collect fresh data
    data, err := bus.collectors[metricType].Collect(context.Background())
    if err != nil {
        return nil, err
    }
    
    // Update cache
    bus.cache[metricType] = CachedResult{
        Data:      data,
        Timestamp: time.Now(),
        TTL:       bus.getTTL(metricType),
    }
    
    // Notify subscribers
    bus.publish(metricType, data)
    
    return data, nil
}
```

### Subscription Patterns

#### Hardware Manager (Batch Subscriber)
```go
// Wants everything at once, infrequently
func (hw *HardwareManager) Subscribe(bus *CollectionBus) {
    ticker := time.NewTicker(5 * time.Minute)
    
    go func() {
        for range ticker.C {
            snapshot := bus.GetFullSnapshot() // All hardware info
            hw.BuildGraph(snapshot)
        }
    }()
}
```

#### Metrics Manager (Stream Subscriber)
```go
// Wants specific metrics at different rates
func (m *MetricsManager) Subscribe(bus *CollectionBus) {
    cpuChan := bus.Subscribe(MetricTypeCPUStats, 1*time.Second)
    memChan := bus.Subscribe(MetricTypeMemoryStats, 10*time.Second)
    
    go func() {
        for {
            select {
            case cpu := <-cpuChan:
                m.exportCPUMetrics(cpu)
            case mem := <-memChan:
                m.exportMemoryMetrics(mem)
            }
        }
    }()
}
```

## Benefits

1. **Efficiency**: Collect once, use multiple times
2. **Coordination**: No duplicate reads of same files
3. **Flexibility**: Different consumers, different rates
4. **Caching**: Natural deduplication point
5. **Testability**: Mock the bus, not individual collectors

## Challenges

1. **Memory Pressure**: Caching everything could be expensive
2. **Timing Complexity**: Coordinating multiple schedules
3. **Back Pressure**: Slow consumers could block fast ones
4. **Error Handling**: One failed collector affects multiple consumers

## Alternative: Simple Pub-Sub

```go
// Simpler but less efficient
type SimpleBus struct {
    *pubsub.Bus
}

// Each manager still triggers collection
// But results are published to shared bus
perfManager.Collect(CPUStats) // Publishes to bus
// Hardware manager receives it if subscribed
```

## Recommendation

Start simple:
1. Keep managers separate (as is)
2. Add optional shared cache for expensive operations
3. Monitor for actual duplicate collection problems
4. Implement full bus only if performance requires it

The full collection bus is elegant but might be premature optimization. The current duplicate collection might be negligible compared to the complexity of coordination.

## Next Steps

If we proceed with collection bus:
1. Define clear ownership (who triggers collection?)
2. Implement backpressure handling
3. Add metrics to measure actual duplicate collection
4. Consider using existing pubsub library vs custom implementation