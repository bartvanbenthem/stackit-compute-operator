package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/bartvanbenthem/stackit-compute-operator/internal/stackit"
)

const (
	readyConditionType = "Ready"

	// pollInterval is the default requeue delay used while actively waiting
	// on a transitional external state, and by demoteTransientAuthError's
	// short retry after a transient STACKIT auth failure. Server and Cluster
	// override this with a longer, resource-specific poll interval.
	pollInterval  = 10 * time.Second
	errorInterval = time.Minute
	resyncPeriod  = 5 * time.Minute
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

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// labelsEqual reports whether every desired label is present in current
// with the same value. Extra labels present only in current (e.g. STACKIT's
// own "stackit-" prefixed labels) are ignored.
func labelsEqual(current map[string]interface{}, desired map[string]string) bool {
	for k, v := range desired {
		cv, ok := current[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", cv) != v {
			return false
		}
	}
	return true
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
// (see stackit.IsTransientAuthError) from a hard reconcile error to an
// info-level log plus a fixed short requeue. Left as a normal error,
// controller-runtime would log it at error level with a stack trace and
// apply its exponential backoff, even though this specific failure reliably
// clears on the very next attempt. Logged at the default level (not V(1))
// so it's visible without extra flags - a silent retry loop here previously
// made real clock-skew problems indistinguishable from "nothing is happening".
func demoteTransientAuthError(ctx context.Context, result ctrl.Result, err error) (ctrl.Result, error) {
	if err == nil || !stackit.IsTransientAuthError(err) {
		return result, err
	}
	log.FromContext(ctx).Info("STACKIT auth token rejected (invalid iat, likely clock skew), retrying automatically", "detail", err.Error())
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// idAlreadyPresent re-fetches obj directly from the API server, bypassing
// the informer cache, and reports whether getID (a closure reading obj's
// now-refreshed status) returns a non-empty ID. reconcileCreate calls this
// immediately before issuing a create request to STACKIT: unlike the
// Cluster API (an upsert keyed by name, see stackit.IsAlreadyExists),
// Server/Network/Volume creates are plain ID-generating POSTs, so two
// reconciles racing on a stale cached view of Status.<ID> == "" would
// otherwise each create a separate, orphaned STACKIT resource rather than
// erroring.
func idAlreadyPresent(ctx context.Context, reader client.Reader, obj client.Object, getID func() string) (bool, error) {
	if err := reader.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return false, err
	}
	return getID() != "", nil
}

// statusUnchanged reports whether a status value snapshotted before
// reconciling still matches after observed state was applied to it. Callers
// use this to skip a Status().Update() write - and the resourceVersion bump
// and watch event it causes - when polling STACKIT found nothing new, which
// is the common case on every transitional-state or resync poll.
func statusUnchanged(before, after interface{}) bool {
	return equality.Semantic.DeepEqual(before, after)
}
