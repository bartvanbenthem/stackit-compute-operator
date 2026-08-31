// Command manager runs the stackit-compute-operator controller manager, which
// reconciles compute.sostackit.dev/v1alpha1 Server, Volume, and Network
// resources against the STACKIT Compute Engine (IaaS) API, and Cluster
// resources against the STACKIT Kubernetes Engine (SKE) API.
package main

import (
	"flag"
	"os"

	// Import all Kubernetes client-go auth plugins so the manager can run
	// against clusters that require them (e.g. OIDC, exec-based auth).
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
	"github.com/bartvanbenthem/stackit-compute-operator/internal/controller"
	"github.com/bartvanbenthem/stackit-compute-operator/internal/stackit"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(computev1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", "127.0.0.1:8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this ensures there is only one active controller manager.")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "stackit-compute-operator.compute.sostackit.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	stackitClient, err := stackit.NewClient()
	if err != nil {
		setupLog.Error(err, "unable to create STACKIT API client; check STACKIT_SERVICE_ACCOUNT_KEY_PATH / STACKIT_PRIVATE_KEY_PATH / STACKIT_SERVICE_ACCOUNT_TOKEN")
		os.Exit(1)
	}

	skeClient, err := stackit.NewSKEClient()
	if err != nil {
		setupLog.Error(err, "unable to create STACKIT SKE API client; check STACKIT_SERVICE_ACCOUNT_KEY_PATH / STACKIT_PRIVATE_KEY_PATH / STACKIT_SERVICE_ACCOUNT_TOKEN")
		os.Exit(1)
	}

	if err := (&controller.ServerReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: stackitClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Server")
		os.Exit(1)
	}

	if err := (&controller.VolumeReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: stackitClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Volume")
		os.Exit(1)
	}

	if err := (&controller.NetworkReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: stackitClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Network")
		os.Exit(1)
	}

	if err := (&controller.ClusterReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: skeClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
