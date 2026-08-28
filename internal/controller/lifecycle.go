package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/bartvanbenthem/stackit-compute-operator/internal/stackit"
)

// isAdopted reports whether a resource references an existing STACKIT
// object via spec.existingId rather than being created and owned by the
// operator.
func isAdopted(existingID *string) bool {
	return existingID != nil && *existingID != ""
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func derefBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}

// namespaceOrClusterScoped returns ns for display in logs, substituting a
// placeholder when ns is empty (cluster-scoped resources have no namespace).
func namespaceOrClusterScoped(ns string) string {
	if ns == "" {
		return "<cluster-scoped>"
	}
	return ns
}

// ensureFinalizer adds finalizer to obj and persists the change if it isn't
// already present. It reports whether the finalizer was added.
func ensureFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string) (bool, error) {
	if controllerutil.ContainsFinalizer(obj, finalizer) {
		return false, nil
	}
	controllerutil.AddFinalizer(obj, finalizer)
	if err := c.Update(ctx, obj); err != nil {
		return false, err
	}
	return true, nil
}

// removeFinalizerAndUpdate removes finalizer from obj and persists the
// change. It is a no-op if the finalizer isn't present.
func removeFinalizerAndUpdate(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}
	controllerutil.RemoveFinalizer(obj, finalizer)
	return c.Update(ctx, obj)
}

// demoteTransientAuthError downgrades a transient STACKIT auth token error
// (see stackit.IsTransientAuthError) from a hard reconcile error to a
// debug-level log plus a fixed short requeue. Left as a normal error,
// controller-runtime would log it at error level with a stack trace and
// apply its exponential backoff, even though this specific failure reliably
// clears on the very next attempt. Logged at V(1) rather than dropped
// entirely so it's still visible when diagnosing repeated token rejections.
func demoteTransientAuthError(ctx context.Context, result ctrl.Result, err error) (ctrl.Result, error) {
	if err == nil || !stackit.IsTransientAuthError(err) {
		return result, err
	}
	log.FromContext(ctx).V(1).Info("STACKIT auth token rejected, retrying automatically", "detail", err.Error())
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// statusUnchanged reports whether a status value snapshotted before
// reconciling still matches after observed state was applied to it. Callers
// use this to skip a Status().Update() write - and the resourceVersion bump
// and watch event it causes - when polling STACKIT found nothing new, which
// is the common case on every transitional-state or resync poll.
func statusUnchanged(before, after interface{}) bool {
	return equality.Semantic.DeepEqual(before, after)
}
