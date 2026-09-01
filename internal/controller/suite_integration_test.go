//go:build integration

// Package controller integration suite. Spins up a real kube-apiserver and
// etcd via envtest and runs the actual controller-runtime manager against
// them, so it exercises finalizer handling, the status subresource, and
// requeue timing the way production does. The STACKIT side is still faked
// (there is no sandboxed STACKIT API to test against), but through the real
// SDK types via iaas.DefaultAPIServiceMock, not a hand-rolled interface.
//
// Run with `make test-integration`; it downloads envtest binaries (etcd,
// kube-apiserver) on first run and is too slow/networked for the default
// `make test` target.
package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

var (
	testEnv        *envtest.Environment
	k8sClient      client.Client
	testScheme     *runtime.Scheme
	cancelMgr      context.CancelFunc
	volumeBackend  *fakeVolumeBackend
	networkBackend *fakeNetworkBackend
	clusterBackend *fakeClusterBackend
)

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	testScheme = runtime.NewScheme()
	utilruntime.Must(computev1alpha1.AddToScheme(testScheme))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting envtest environment: %v\n", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  testScheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating manager: %v\n", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}
	k8sClient = mgr.GetClient()

	backend := newFakeStackitBackend()
	if err := (&ServerReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: &iaas.APIClient{DefaultAPI: backend.mock()},
		APIReader:     mgr.GetAPIReader(),
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "setting up controller: %v\n", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}

	volumeBackend = newFakeVolumeBackend()
	if err := (&VolumeReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: &iaas.APIClient{DefaultAPI: volumeBackend.mock()},
		APIReader:     mgr.GetAPIReader(),
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "setting up controller: %v\n", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}

	networkBackend = newFakeNetworkBackend()
	if err := (&NetworkReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: &iaas.APIClient{DefaultAPI: networkBackend.mock()},
		APIReader:     mgr.GetAPIReader(),
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "setting up controller: %v\n", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}

	clusterBackend = newFakeClusterBackend()
	if err := (&ClusterReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		StackitClient: &ske.APIClient{DefaultAPI: clusterBackend.mock()},
	}).SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "setting up controller: %v\n", err)
		_ = testEnv.Stop()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelMgr = cancel
	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "manager exited with error: %v\n", err)
		}
	}()

	code := m.Run()

	cancelMgr()
	if err := testEnv.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "stopping envtest environment: %v\n", err)
	}
	os.Exit(code)
}
