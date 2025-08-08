// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package main demonstrates the capability-aware collector registration system
package main

import (
	"fmt"
	"os"
	"runtime"
	"text/tabwriter"

	"github.com/antimetal/agent/pkg/performance"
	_ "github.com/antimetal/agent/pkg/performance/collectors" // Import to trigger init() functions
)

func main() {
	fmt.Printf("Antimetal Agent - Collector Status Report\n")
	fmt.Printf("Platform: %s/%s\n\n", runtime.GOOS, runtime.GOARCH)

	// Get available and unavailable collectors
	available := performance.GetAvailableCollectors()
	unavailable := performance.GetUnavailableCollectors()

	// Create a tabwriter for nice formatting
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Print available collectors
	fmt.Fprintf(w, "AVAILABLE COLLECTORS (%d):\n", len(available))
	fmt.Fprintf(w, "Metric Type\tStatus\tDescription\n")
	fmt.Fprintf(w, "-----------\t------\t-----------\n")
	
	for _, metricType := range available {
		fmt.Fprintf(w, "%s\t✓\tReady to use\n", metricType)
	}
	
	// Print unavailable collectors
	if len(unavailable) > 0 {
		fmt.Fprintf(w, "\nUNAVAILABLE COLLECTORS (%d):\n", len(unavailable))
		fmt.Fprintf(w, "Metric Type\tReason\tMissing Capabilities\tMin Kernel\n")
		fmt.Fprintf(w, "-----------\t------\t-------------------\t----------\n")
		
		for metricType, info := range unavailable {
			capNames := ""
			if len(info.MissingCapabilities) > 0 {
				caps := make([]string, len(info.MissingCapabilities))
				for i, cap := range info.MissingCapabilities {
					caps[i] = cap.String()
				}
				capNames = fmt.Sprintf("%v", caps)
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", 
				metricType, 
				info.Reason,
				capNames,
				info.MinKernelVersion,
			)
		}
	}
	
	w.Flush()
	
	// Example of checking a specific collector
	fmt.Println("\nDETAILED STATUS CHECK:")
	collectors := []performance.MetricType{
		performance.MetricTypeKernel,
		performance.MetricTypeProcess,
		performance.MetricTypeLoad,
	}
	
	for _, mt := range collectors {
		available, reason := performance.GetCollectorStatus(mt)
		fmt.Printf("- %s: available=%v (%s)\n", mt, available, reason)
	}
	
	// Summary
	fmt.Printf("\nSUMMARY:\n")
	fmt.Printf("- Total collectors: %d\n", len(available)+len(unavailable))
	fmt.Printf("- Available: %d\n", len(available))
	fmt.Printf("- Unavailable: %d\n", len(unavailable))
	
	if runtime.GOOS == "linux" && len(unavailable) > 0 {
		fmt.Printf("\nNOTE: Some collectors require additional Linux capabilities.\n")
		fmt.Printf("Run with appropriate privileges or in a container with the required capabilities.\n")
	}
}