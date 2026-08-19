package v1alpha1

// LocalObjectReference references another compute.stackitvm.dev object in
// the same namespace.
type LocalObjectReference struct {
	// Name of the referenced object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
