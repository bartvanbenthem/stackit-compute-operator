//go:build integration

package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

// eventually polls cond until it returns true or timeout elapses, failing
// the test otherwise. Kept local rather than pulling in gomega, since this
// is the only place that needs it.
func eventually(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for: %s", timeout, msg)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestIntegration_ServerLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-server", Namespace: "default"}

	server := &computev1alpha1.Server{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.ServerSpec{
			ProjectId:   "11111111-1111-1111-1111-111111111111",
			Region:      "eu01",
			MachineType: "c1.2",
			ImageId:     "22222222-2222-2222-2222-222222222222",
			NetworkId:   "33333333-3333-3333-3333-333333333333",
		},
	}

	if err := k8sClient.Create(ctx, server); err != nil {
		t.Fatalf("creating Server: %v", err)
	}

	// The controller should observe the new object, add the finalizer,
	// trigger creation against the (fake) STACKIT backend, and eventually
	// report the server as active.
	eventually(t, 30*time.Second, "server becomes Ready=True/Active", func() bool {
		got := &computev1alpha1.Server{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.ServerId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Active"
	})

	got := &computev1alpha1.Server{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Server: %v", err)
	}
	if got.Status.PowerStatus != "RUNNING" {
		t.Errorf("Status.PowerStatus = %q, want RUNNING", got.Status.PowerStatus)
	}

	// Request the server be powered off and confirm the controller drives
	// it there and reflects that back onto status.
	got.Spec.PowerState = computev1alpha1.PowerStateInactive
	if err := k8sClient.Update(ctx, got); err != nil {
		t.Fatalf("requesting power off: %v", err)
	}

	eventually(t, 30*time.Second, "server becomes Ready=True/Inactive", func() bool {
		cur := &computev1alpha1.Server{}
		if err := k8sClient.Get(ctx, name, cur); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(cur.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Inactive" &&
			cur.Status.PowerStatus == "STOPPED"
	})

	// Delete the CR and confirm the controller drives deletion against
	// STACKIT before letting the finalizer clear and the object disappear.
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Server: %v", err)
	}

	eventually(t, 30*time.Second, "server object is fully removed", func() bool {
		cur := &computev1alpha1.Server{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}
