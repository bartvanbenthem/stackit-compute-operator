package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
