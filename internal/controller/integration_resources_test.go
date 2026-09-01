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

func TestIntegration_VolumeLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-volume", Namespace: "default"}

	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.VolumeSpec{
			ProjectId:        "11111111-1111-1111-1111-111111111111",
			Region:           "eu01",
			AvailabilityZone: "eu01-1",
			Size:             32,
		},
	}
	if err := k8sClient.Create(ctx, volume); err != nil {
		t.Fatalf("creating Volume: %v", err)
	}

	eventually(t, 30*time.Second, "volume becomes Ready=True/Available", func() bool {
		got := &computev1alpha1.Volume{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.VolumeId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Available"
	})

	got := &computev1alpha1.Volume{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Volume: %v", err)
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Volume: %v", err)
	}

	eventually(t, 30*time.Second, "volume object is fully removed", func() bool {
		cur := &computev1alpha1.Volume{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

func TestIntegration_NetworkLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-network", Namespace: "default"}

	network := &computev1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.NetworkSpec{
			ProjectId: "11111111-1111-1111-1111-111111111111",
			Region:    "eu01",
		},
	}
	if err := k8sClient.Create(ctx, network); err != nil {
		t.Fatalf("creating Network: %v", err)
	}

	eventually(t, 30*time.Second, "network becomes Ready=True/Created", func() bool {
		got := &computev1alpha1.Network{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.NetworkId == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Created"
	})

	got := &computev1alpha1.Network{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Network: %v", err)
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Network: %v", err)
	}

	eventually(t, 30*time.Second, "network object is fully removed", func() bool {
		cur := &computev1alpha1.Network{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

func TestIntegration_ClusterLifecycle(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-cluster", Namespace: "default"}

	cluster := &computev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.ClusterSpec{
			ProjectId:         "11111111-1111-1111-1111-111111111111",
			Region:            "eu01",
			KubernetesVersion: "1.29.3",
			NodePools: []computev1alpha1.NodePoolSpec{
				{
					Name:                "pool-1",
					MachineType:         "c1.2",
					MachineImageName:    "flatcar",
					MachineImageVersion: "3815.2.0",
					AvailabilityZones:   []string{"eu01-1"},
					Minimum:             1,
					Maximum:             3,
					Volume:              computev1alpha1.NodePoolVolumeSpec{Size: 32},
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("creating Cluster: %v", err)
	}

	eventually(t, 30*time.Second, "cluster becomes Ready=True/Healthy", func() bool {
		got := &computev1alpha1.Cluster{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		if got.Status.ClusterName == "" {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Healthy"
	})

	got := &computev1alpha1.Cluster{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting Cluster: %v", err)
	}
	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting Cluster: %v", err)
	}

	eventually(t, 30*time.Second, "cluster object is fully removed", func() bool {
		cur := &computev1alpha1.Cluster{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})
}

// TestIntegration_AdoptExistingCluster mirrors
// TestIntegration_AdoptExistingVolume for Cluster: a Cluster with
// spec.existingClusterName set must only observe the pre-seeded
// STACKIT-side cluster (no finalizer, no CreateOrUpdateCluster call), and
// deleting the CR must NOT delete the underlying cluster.
func TestIntegration_AdoptExistingCluster(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-adopted-cluster", Namespace: "default"}
	const existingName = "preexisting-cluster"

	clusterBackend.seedExisting(existingName)

	cluster := &computev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.ClusterSpec{
			ProjectId:           "11111111-1111-1111-1111-111111111111",
			Region:              "eu01",
			KubernetesVersion:   "1.29.3",
			ExistingClusterName: ptr(existingName),
		},
	}
	if err := k8sClient.Create(ctx, cluster); err != nil {
		t.Fatalf("creating adopted Cluster: %v", err)
	}

	eventually(t, 30*time.Second, "adopted cluster becomes Ready=True/Healthy", func() bool {
		got := &computev1alpha1.Cluster{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Healthy" &&
			got.Status.ClusterName == existingName
	})

	got := &computev1alpha1.Cluster{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting adopted Cluster: %v", err)
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("Finalizers = %v, want none on an adopted resource", got.Finalizers)
	}

	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting adopted Cluster: %v", err)
	}
	eventually(t, 30*time.Second, "adopted cluster CR is fully removed", func() bool {
		cur := &computev1alpha1.Cluster{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})

	if !clusterBackend.existsLocked() {
		t.Error("STACKIT-side cluster was deleted, want it left untouched (adopted, not owned)")
	}
}

// TestIntegration_AdoptExistingVolume exercises the "not the owner" path
// against a real apiserver: a Volume with spec.existingId set must only
// observe the pre-seeded STACKIT-side volume (no finalizer, no Create
// call), and deleting the CR must NOT delete the underlying volume.
func TestIntegration_AdoptExistingVolume(t *testing.T) {
	ctx := context.Background()
	name := types.NamespacedName{Name: "it-adopted-volume", Namespace: "default"}
	const existingID = "99999999-8888-7777-6666-555555555555"

	volumeBackend.seedExisting(existingID, 128)

	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
		Spec: computev1alpha1.VolumeSpec{
			ProjectId:        "11111111-1111-1111-1111-111111111111",
			Region:           "eu01",
			AvailabilityZone: "eu01-1",
			ExistingID:       ptr(existingID),
		},
	}
	if err := k8sClient.Create(ctx, volume); err != nil {
		t.Fatalf("creating adopted Volume: %v", err)
	}

	eventually(t, 30*time.Second, "adopted volume becomes Ready=True/Available", func() bool {
		got := &computev1alpha1.Volume{}
		if err := k8sClient.Get(ctx, name, got); err != nil {
			return false
		}
		cond := meta.FindStatusCondition(got.Status.Conditions, readyConditionType)
		return cond != nil && cond.Status == metav1.ConditionTrue && cond.Reason == "Available" &&
			got.Status.VolumeId == existingID && got.Status.Size == 128
	})

	got := &computev1alpha1.Volume{}
	if err := k8sClient.Get(ctx, name, got); err != nil {
		t.Fatalf("getting adopted Volume: %v", err)
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("Finalizers = %v, want none on an adopted resource", got.Finalizers)
	}

	if err := k8sClient.Delete(ctx, got); err != nil {
		t.Fatalf("deleting adopted Volume: %v", err)
	}
	eventually(t, 30*time.Second, "adopted volume CR is fully removed", func() bool {
		cur := &computev1alpha1.Volume{}
		err := k8sClient.Get(ctx, name, cur)
		return errors.IsNotFound(err)
	})

	if !volumeBackend.existsLocked() {
		t.Error("STACKIT-side volume was deleted, want it left untouched (adopted, not owned)")
	}
}

func ptr[T any](v T) *T { return &v }
