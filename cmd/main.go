// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"crypto/tls"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/antimetal/agent/internal/hardware"
	"github.com/antimetal/agent/internal/intake"
	k8sagent "github.com/antimetal/agent/internal/kubernetes/agent"
	"github.com/antimetal/agent/internal/kubernetes/cluster"
	"github.com/antimetal/agent/internal/kubernetes/scheme"
	"github.com/antimetal/agent/internal/runtime"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/resource/store"
)

var (
	setupLog logr.Logger

	// CLI Options
	intakeAddr             string
	intakeAPIKey           string
	intakeSecure           bool
	metricsAddr            string
	metricsSecure          bool
	metricsCertDir         string
	metricsCertName        string
	metricsKeyName         string
	enableLeaderElection   bool
	probeAddr              string
	enableHTTP2            bool
	enableK8sController    bool
	kubernetesProvider     string
	eksAccountID           string
	eksRegion              string
	eksClusterName         string
	eksAutodiscover        bool
	maxStreamAge           time.Duration
	pprofAddr              string
	dataDir                string
	hardwareUpdateInterval time.Duration
	runtimeUpdateInterval  time.Duration
)

func init() {
	flag.StringVar(&intakeAddr, "intake-address", "intake.antimetal.com:443",
		"The address of the intake service",
	)
	flag.StringVar(&intakeAPIKey, "intake-api-key", "",
		"The API key to use upload resources",
	)
	flag.BoolVar(&intakeSecure, "intake-secure", true,
		"Use secure connection to the Antimetal intake service",
	)
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
	flag.BoolVar(&enableK8sController, "enable-kubernetes-controller", true,
		"Enable Kubernetes cluster snapshot collector")
	flag.StringVar(&kubernetesProvider, "kubernetes-provider", "kind", "The Kubernetes provider")
	flag.StringVar(&eksAccountID, "kubernetes-provider-eks-account-id", "",
		"The AWS account ID the EKS cluster is deployed in")
	flag.StringVar(&eksRegion, "kubernetes-provider-eks-region", "",
		"The AWS region the EKS cluster is deployed in")
	flag.StringVar(&eksClusterName, "kubernetes-provider-eks-cluster-name", "",
		"The name of the EKS cluster")
	flag.BoolVar(&eksAutodiscover, "kubernetes-provider-eks-autodiscover", true,
		"Autodiscover EKS cluster name")
	flag.DurationVar(&maxStreamAge, "max-stream-age", 10*time.Minute,
		"Maximum age of the intake stream before it is reset")
	flag.StringVar(&pprofAddr, "pprof-address", "0",
		"The address the pprof server binds to. Set this to '0' to disable the pprof server")
	flag.StringVar(&dataDir, "data-directory", "/var/lib/antimetal",
		"The directory where the agent will place its persistent data files. Set to empty string for in-memory mode.")
	flag.DurationVar(&hardwareUpdateInterval, "hardware-update-interval", 5*time.Minute,
		"Interval for hardware topology discovery updates")
	flag.DurationVar(&runtimeUpdateInterval, "runtime-update-interval", 30*time.Second,
		"Interval for container and process discovery updates")

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog = ctrl.Log.WithName("setup")
}

func main() {
	ctx := ctrl.SetupSignalHandler()

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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme.Get(),
		Metrics:                metricsServerOpts,
		HealthProbeBindAddress: probeAddr,
		PprofBindAddress:       pprofAddr,
		LeaderElection:         enableLeaderElection,
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
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Shared resources
	var rsrcDataDir string
	if dataDir != "" {
		rsrcDataDir = filepath.Join(dataDir, "resource")
		setupLog.V(1).Info("using disk storage for resource store", "path", rsrcDataDir)
	} else {
		setupLog.V(1).Info("using in-memory storage for resource store")
	}
	rsrcStore, err := store.New(rsrcDataDir)
	if err != nil {
		setupLog.Error(err, "unable to create resource inventory")
		os.Exit(1)
	}
	if err := mgr.Add(rsrcStore); err != nil {
		setupLog.Error(err, "unable to register resource inventory")
		os.Exit(1)
	}

	var creds credentials.TransportCredentials
	if intakeSecure {
		creds = credentials.NewTLS(&tls.Config{})
	} else {
		creds = insecure.NewCredentials()
	}
	intakeConn, err := grpc.NewClient(intakeAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: 5 * time.Minute,
		}),
	)
	if err != nil {
		setupLog.Error(err, "unable to connect to intake service")
		os.Exit(1)
	}
	defer func() {
		if err := intakeConn.Close(); err != nil {
			setupLog.Error(err, "unable to close intake service connection")
		}
	}()

	// Setup Intake Worker
	intakeWorker, err := intake.NewWorker(rsrcStore,
		intake.WithLogger(mgr.GetLogger().WithName("intake-worker")),
		intake.WithGRPCConn(intakeConn),
		intake.WithAPIKey(intakeAPIKey),
		intake.WithMaxStreamAge(maxStreamAge),
	)
	if err != nil {
		setupLog.Error(err, "unable to create intake worker")
		os.Exit(1)
	}
	if err := mgr.Add(intakeWorker); err != nil {
		setupLog.Error(err, "unable to register intake worker")
		os.Exit(1)
	}

	// Setup Performance Manager (for hardware discovery)
	perfManager, err := performance.NewManager(performance.ManagerOptions{
		Logger:      mgr.GetLogger().WithName("performance-manager"),
		NodeName:    os.Getenv("NODE_NAME"),
		ClusterName: "",
	})
	if err != nil {
		setupLog.Error(err, "unable to create performance manager")
		os.Exit(1)
	}

	// Setup Hardware Manager
	hwManager, err := hardware.NewManager(
		mgr.GetLogger().WithName("hardware-manager"),
		hardware.ManagerConfig{
			Store:              rsrcStore,
			PerformanceManager: perfManager,
			UpdateInterval:     hardwareUpdateInterval,
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

	// Setup Runtime Manager (for container/process discovery)
	rtManager, err := runtime.NewManager(
		mgr.GetLogger().WithName("runtime-manager"),
		runtime.ManagerConfig{
			Store:              rsrcStore,
			PerformanceManager: perfManager,
			UpdateInterval:     runtimeUpdateInterval,
		},
	)
	if err != nil {
		setupLog.Error(err, "unable to create runtime manager")
		os.Exit(1)
	}
	if err := mgr.Add(rtManager); err != nil {
		setupLog.Error(err, "unable to register runtime manager")
		os.Exit(1)
	}

	// Setup Kubernetes Collector Controller
	if enableK8sController {
		providerOpts := getProviderOptions(setupLog.WithName("cluster-provider"))
		provider, err := cluster.GetProvider(ctx, kubernetesProvider, providerOpts)
		if err != nil {
			setupLog.Error(err, "unable to determine cluster provider")
			os.Exit(1)
		}
		ctrl := &k8sagent.Controller{
			Provider: provider,
			Store:    rsrcStore,
		}
		if err := ctrl.SetupWithManager(mgr); err != nil {
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

func getProviderOptions(logger logr.Logger) cluster.ProviderOptions {
	return cluster.ProviderOptions{
		Logger: logger,
		EKS: cluster.EKSOptions{
			Autodiscover: eksAutodiscover,
			AccountID:    eksAccountID,
			Region:       eksRegion,
			ClusterName:  eksClusterName,
		},
	}
}
