# Profiling Quick Start Guide

## TL;DR - Get Profiling into Datadog in 3 Steps

### 1. Enable Datadog Profiling Consumer

```bash
./agent --enable-profiling-datadog
```

That's it! Profiles will flow to your Datadog Agent and appear in the Continuous Profiler UI.

### 2. View in Datadog

1. Open Datadog UI
2. Navigate to **APM → Profiling**
3. Filter by service: `antimetal-agent`
4. View flame graphs! 🔥

### 3. Customize (Optional)

```bash
./agent \
  --enable-profiling-datadog \
  --profiling-datadog-service=my-custom-name \
  --profiling-datadog-env=staging
```

## Prerequisites

### Datadog Agent Must Be Running

**Check if Agent is reachable:**
```bash
curl http://localhost:8126/profiling/v1/input
# Should return: 405 Method Not Allowed (POST required)
```

**If not working:**
```bash
# Ensure Agent has profiling enabled
DD_PROFILING_ENABLED=true
DD_APM_ENABLED=true
```

### Profiler Must Be Configured

Create a `ProfileCollectionConfig` in your config directory:

```yaml
# config/my-profiler.yaml
apiVersion: config.antimetal.com/v1
kind: ProfileCollectionConfig
metadata:
  name: cpu-profiler
spec:
  event_name: cpu-cycles      # or cpu-clock, cache-misses, etc.
  interval_seconds: 10        # Collect every 10 seconds
  sample_period: 1000000      # Sample every 1M cycles
```

## Common Scenarios

### Scenario 1: Datadog Only (Recommended for Most)

```bash
./agent --enable-profiling-datadog
```

**When to use:**
- You already use Datadog for observability
- You want production-ready profiling UI
- You need alerting on performance regressions

### Scenario 2: Local Development

```bash
./agent \
  --enable-profiling-perfdata \
  --profiling-perfdata-path=/tmp/profiles
```

Then generate flamegraphs:
```bash
cd /tmp/profiles
perf script -i perf-*.data | \
  stackcollapse-perf.pl | \
  flamegraph.pl > flame.svg
open flame.svg
```

**When to use:**
- Local debugging
- Offline analysis
- No Datadog access

### Scenario 3: Multi-Backend (Power Users)

```bash
./agent \
  --enable-profiling-datadog \
  --enable-profiling-perfdata \
  --profiling-perfdata-path=/var/lib/antimetal/profiles
```

**When to use:**
- Want Datadog UI for production
- Also want local perf.data for deep dives
- Maximum flexibility

### Scenario 4: OTLP Backends (Future)

```bash
./agent \
  --enable-profiling-otlp \
  --profiling-otlp-endpoint=pyroscope:4317
```

**When to use:**
- Using Grafana Pyroscope
- Using Honeycomb
- Any OTLP-compatible profiling backend

**Note:** Requires gRPC export implementation (currently placeholder)

## Verifying It's Working

### 1. Check Logs

```bash
# Look for consumer startup
./agent 2>&1 | grep "profiling consumer started"
# Output: Datadog profiling consumer started and registered

# Look for uploads
./agent 2>&1 | grep "uploading profiles"
# Output: uploading profiles to Datadog batch_size=X
```

### 2. Check Datadog Agent

```bash
# Agent should show profile intake
tail -f /var/log/datadog/agent.log | grep profile
```

### 3. Check Datadog UI

After 1-2 minutes:
1. APM → Profiling
2. Filter: service=`antimetal-agent`
3. Should see flame graphs!

## Troubleshooting

### No Profiles in Datadog

**Check 1:** Is profiler running?
```bash
# Verify ProfileCollectionConfig exists
kubectl get profilecollectionconfigs
```

**Check 2:** Is consumer enabled?
```bash
# Look for flag in command args
ps aux | grep enable-profiling-datadog
```

**Check 3:** Is Agent reachable?
```bash
curl http://localhost:8126/profiling/v1/input
# Should not return "connection refused"
```

**Check 4:** Check consumer logs
```bash
# Look for errors
./agent 2>&1 | grep "unable to.*Datadog"
```

### Profiles Dropping

```
"dropping profile event, buffer full"
```

**Solution:** Profiler is generating data faster than uploads. Either:
- Increase buffer size (in config.go)
- Increase upload frequency (decrease UploadInterval)
- Reduce profiling interval

### Upload Failures

```
"failed to upload profile: Post ... connection refused"
```

**Solution:** Datadog Agent not running or not listening on expected port

```
"unexpected status 400"
```

**Solution:** Agent rejecting profiles - check Agent has profiling enabled

## Advanced Configuration

### Custom Upload Frequency

Edit `cmd/main.go` to change upload interval:

```go
datadogConfig := datadog.Config{
    UploadInterval: 30 * time.Second,  // Upload every 30s instead of 60s
    // ...
}
```

### Custom Tags

```go
datadogConfig.Tags = map[string]string{
    "region": "us-east-1",
    "team":   "infrastructure",
}
```

### Multiple Environments

```bash
# Production
./agent --profiling-datadog-env=production

# Staging
./agent --profiling-datadog-env=staging

# Development
./agent --profiling-datadog-env=dev
```

Filter by environment in Datadog UI!

## What's Next?

### For Datadog Integration (Nothing! It works!)
The Datadog consumer is **production-ready** and fully functional. Just:
1. Enable the flag
2. Ensure Agent is running
3. Watch profiles appear in UI

### For OTLP Integration (Optional)
If you want to send to OTLP backends:
1. Implement gRPC client in `otlpprofiles/consumer.go`
2. Follow pattern from `internal/metrics/consumers/otel/consumer.go`
3. ~50 lines of code

### For Symbol Resolution (Optional)
Currently stacks show as hex addresses. To add function names:
1. Parse `/proc/[pid]/maps`
2. Read ELF symbols with `debug/elf`
3. Cache resolutions

**But:** Many backends (including Datadog) can symbolize server-side!

---

**Quick Start:** `--enable-profiling-datadog`
**Documentation:** See `PROFILING_DATADOG_INTEGRATION.md`
**Architecture:** See `PROFILING_CONSUMERS_IMPLEMENTATION.md`
