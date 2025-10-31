// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	clientconfig "k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/antimetal/agent/internal/config"
	"github.com/antimetal/agent/internal/containers"
	"github.com/antimetal/agent/internal/endpoints"
	"github.com/antimetal/agent/internal/hardware"
	"github.com/antimetal/agent/internal/intake"
	k8sagent "github.com/antimetal/agent/internal/kubernetes/agent"
	"github.com/antimetal/agent/internal/kubernetes/scheme"
	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/internal/metrics/consumers/debug"
	"github.com/antimetal/agent/internal/metrics/consumers/otel"
	perfmanager "github.com/antimetal/agent/internal/perf/manager"
	"github.com/antimetal/agent/internal/resource/store"
	"github.com/antimetal/agent/internal/runtime"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/config/environment"
	"github.com/antimetal/agent/pkg/performance"
)

var (
	setupLog logr.Logger

	// CLI Options (alphabetical order)
	enableHTTP2          bool
	enableLeaderElection bool
	metricsAddr          string
	metricsCertDir       string
	metricsCertName      string
	metricsKeyName       string
	metricsSecure        bool
	pprofAddr            string
	probeAddr            string
	enableK8s            bool
)

func init() {
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to. Set this to '0' to disable the metrics server")
	flag.BoolVar(&metricsSecure, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.StringVar(&metricsCertDir, "metrics-cert-dir", "",
		"The directory where the metrics server TLS certificates are stored.",
	)
	flag.StringVar(&metricsCertName, "metrics-cert-name", "",
		"The name of the TLS certificate file for the metrics server.",
	)
	flag.StringVar(&metricsKeyName, "metrics-key-name", "",
		"The name of the TLS key file for the metrics server.",
	)
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the probe endpoint binds to. Set this to '0' to disable the metrics server")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&pprofAddr, "pprof-address", "0",
		"The address the pprof server binds to. Set this to '0' to disable the pprof server")
	flag.BoolVar(&enableK8s, "enable-k8s", true,
		"Enable Kubernetes integration. "+
			"Set to false to run in standalone mode without K8s client, controller, or leader election.")

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog = ctrl.Log.WithName("setup")
}

func createManager(withK8s bool) (manager.Manager, error) {
	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsServerOpts := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: metricsSecure,
		TLSOpts:       tlsOpts,
	}
	if metricsSecure {
		metricsServerOpts.FilterProvider = filters.WithAuthenticationAndAuthorization

		// NOTE: If CertDir, CertName, and KeyName are empty, controller-runtime will
		// automatically generate self-signed certificates for the metrics server. While convenient for
		// development and testing, this setup is not recommended for production.
		metricsServerOpts.CertDir = metricsCertDir
		metricsServerOpts.CertName = metricsCertName
		metricsServerOpts.KeyName = metricsKeyName
	}

	// Load appropriate REST config based on mode
	var restConfig *rest.Config
	var err error
	if withK8s {
		// Load K8s config from in-cluster or kubeconfig
		restConfig, err = clientconfig.NewNonInteractiveDeferredLoadingClientConfig(
			clientconfig.NewDefaultClientConfigLoadingRules(),
			&clientconfig.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubernetes config: %w", err)
		}
	} else {
		// Provide empty config for standalone mode
		restConfig = &rest.Config{}
	}

	return manager.New(restConfig, manager.Options{
		Scheme:                 scheme.Get(),
		Metrics:                metricsServerOpts,
		HealthProbeBindAddress: probeAddr,
		PprofBindAddress:       pprofAddr,
		LeaderElection:         enableLeaderElection && withK8s, // Disable leader election without K8s
		LeaderElectionID:       "4927b366.antimetal.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
}

func main() {
	ctx := ctrl.SetupSignalHandler()

	// Validate mode configuration
	if !enableK8s {
		setupLog.Info("running in standalone mode - Kubernetes integration disabled")
		// Note: K8s controller and intake worker creation is skipped below based on enableK8s flag
	}

	// Create controller-runtime manager (works with or without K8s)
	mgr, err := createManager(enableK8s)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Shared resources
	rsrcStore, err := store.New(
		store.WithLogger(setupLog.WithName("store")),
	)
	if err != nil {
		setupLog.Error(err, "unable to create resource inventory")
		os.Exit(1)
	}
	if err := mgr.Add(rsrcStore); err != nil {
		setupLog.Error(err, "unable to register resource inventory")
		os.Exit(1)
	}

	// Setup Config Manager
	configMgr, err := config.NewManager(
		config.WithLogger(mgr.GetLogger()),
	)
	if err != nil {
		setupLog.Error(err, "unable to create config manager")
		os.Exit(1)
	}
	if err := mgr.Add(configMgr); err != nil {
		setupLog.Error(err, "unable to register config manager")
		os.Exit(1)
	}

	intakeConn, err := endpoints.Intake()
	if err != nil {
		setupLog.Error(err, "unable to connect to intake service")
		os.Exit(1)
	}
	defer func() {
		if err := intakeConn.Close(); err != nil {
			setupLog.Error(err, "unable to close intake service connection")
		}
	}()

	// Setup K8S Intake Worker (leader-only for Kubernetes provider resources)
	// Only create this worker when K8s is enabled
	if enableK8s {
		k8sIntakeWorker, err := intake.NewWorker(rsrcStore,
			intake.WithLogger(mgr.GetLogger().WithName("k8s-intake-worker")),
			intake.WithGRPCConn(intakeConn),
			intake.WithMaxStreamAge(endpoints.MaxStreamAge()),
			intake.WithResourceFilter(&resourcev1.TypeDescriptor{Type: "kubernetes"}),
			intake.WithResourceFilter(&resourcev1.TypeDescriptor{Type: "k8s.io"}),
			intake.WithLeaderElection(true),
		)
		if err != nil {
			setupLog.Error(err, "unable to create K8S intake worker")
			os.Exit(1)
		}
		if err := mgr.Add(k8sIntakeWorker); err != nil {
			setupLog.Error(err, "unable to register K8S intake worker")
			os.Exit(1)
		}
	}

	// Setup Instance Intake Worker (runs on all instances for Antimetal provider resources)
	instanceIntakeWorker, err := intake.NewWorker(rsrcStore,
		intake.WithLogger(mgr.GetLogger().WithName("instance-intake-worker")),
		intake.WithGRPCConn(intakeConn),
		intake.WithMaxStreamAge(endpoints.MaxStreamAge()),
		intake.WithResourceFilter(&resourcev1.TypeDescriptor{Type: "antimetal"}),
		intake.WithLeaderElection(false),
	)
	if err != nil {
		setupLog.Error(err, "unable to create instance intake worker")
		os.Exit(1)
	}
	if err := mgr.Add(instanceIntakeWorker); err != nil {
		setupLog.Error(err, "unable to register instance intake worker")
		os.Exit(1)
	}

	// Setup Metrics Router (if any consumer is enabled)
	var metricsRouter metrics.Router
	enableMetricsPipeline := otel.IsEnabled() || debug.IsEnabled() || perfmanager.Enabled()

	if enableMetricsPipeline {
		router := metrics.NewMetricsRouter(mgr.GetLogger())

		// Register OpenTelemetry consumer if enabled
		if otel.IsEnabled() {
			otelConfig := otel.GetConfigFromEnvironment()
			otelConfig.ServiceVersion = runtime.Version()
			otelConsumer, err := otel.NewConsumer(otelConfig, mgr.GetLogger())
			if err != nil {
				setupLog.Error(err, "unable to create OpenTelemetry consumer")
				os.Exit(1)
			}
			if err := otelConsumer.Start(ctx); err != nil {
				setupLog.Error(err, "unable to start OpenTelemetry consumer")
				os.Exit(1)
			}
			if err := router.RegisterConsumer(otelConsumer); err != nil {
				setupLog.Error(err, "unable to register OpenTelemetry consumer")
				os.Exit(1)
			}
			setupLog.Info("OpenTelemetry consumer started and registered")
		}

		// Register Debug consumer if enabled
		if debug.IsEnabled() {
			debugConfig := debug.GetConfigFromFlags()
			debugConsumer, err := debug.NewConsumer(debugConfig, mgr.GetLogger())
			if err != nil {
				setupLog.Error(err, "unable to create debug consumer")
				os.Exit(1)
			}
			if err := debugConsumer.Start(ctx); err != nil {
				setupLog.Error(err, "unable to start debug consumer")
				os.Exit(1)
			}
			if err := router.RegisterConsumer(debugConsumer); err != nil {
				setupLog.Error(err, "unable to register debug consumer")
				os.Exit(1)
			}
			setupLog.Info("Debug consumer started and registered")
		}

		// Add bus to manager
		if err := mgr.Add(router); err != nil {
			setupLog.Error(err, "unable to register metrics router")
			os.Exit(1)
		}
		metricsRouter = router
		setupLog.Info("Metrics pipeline enabled")
	}

	// Get configuration from environment
	nodeName, err := environment.GetNodeName()
	if err != nil {
		setupLog.Error(err, "unable to get node name")
		os.Exit(1)
	}
	clusterName := environment.GetClusterName()

	// Create a shared collection config with environment overrides
	collectionConfig := performance.DefaultCollectionConfig()
	hostPaths := environment.GetHostPaths()
	collectionConfig.HostProcPath = hostPaths.Proc
	collectionConfig.HostSysPath = hostPaths.Sys
	collectionConfig.HostDevPath = hostPaths.Dev

	// Setup Hardware Manager
	if hardware.Enabled() {
		hwManager, err := hardware.NewManager(
			mgr.GetLogger().WithName("hardware-manager"),
			hardware.ManagerConfig{
				Store:            rsrcStore,
				CollectionConfig: collectionConfig,
				NodeName:         nodeName,
				ClusterName:      clusterName,
			},
		)
		if err != nil {
			setupLog.Error(err, "unable to create hardware manager")
			os.Exit(1)
		}
		if err := mgr.Add(hwManager); err != nil {
			setupLog.Error(err, "unable to register hardware manager")
			os.Exit(1)
		}
	}

	// Setup Performance Manager
	if perfmanager.Enabled() && metricsRouter != nil {
		perfMgr, err := perfmanager.New(
			configMgr,
			metricsRouter,
			perfmanager.WithLogger(mgr.GetLogger()),
		)
		if err != nil {
			setupLog.Error(err, "unable to create performance manager")
			os.Exit(1)
		}
		if err := mgr.Add(perfMgr); err != nil {
			setupLog.Error(err, "unable to register performance manager")
			os.Exit(1)
		}
	}

	// Setup Containers Manager (for container/process discovery)
	if containers.Enabled() {
		containersManager, err := containers.NewManager(
			mgr.GetLogger(),
			containers.ManagerConfig{
				Store: rsrcStore,
			},
		)
		if err != nil {
			setupLog.Error(err, "unable to create containers manager")
			os.Exit(1)
		}
		if err := mgr.Add(containersManager); err != nil {
			setupLog.Error(err, "unable to register containers manager")
			os.Exit(1)
		}
	}

	// Setup Kubernetes Collector Controller (only when K8s is enabled)
	if enableK8s && k8sagent.Enabled() {
		ctrl := &k8sagent.Controller{
			Store: rsrcStore,
		}
		if err := ctrl.SetupWithManager(ctx, mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "K8sCollector")
			os.Exit(1)
		}
	}

	// Final setup and start Manager
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
