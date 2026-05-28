package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+kubebuilder:object:generate=true

// WorkloadTarget identifies which workload to inject hostAliases into.
type WorkloadTarget struct {
	// Namespace of the target workload.
	Namespace string `json:"namespace"`

	// Kind of the workload: Deployment, DaemonSet, or StatefulSet.
	// +kubebuilder:validation:Enum=Deployment;DaemonSet;StatefulSet
	Kind string `json:"kind"`

	// Name of the workload.
	Name string `json:"name"`
}

//+kubebuilder:object:generate=true

// HostAliasEntry represents a single IP-hostname mapping.
type HostAliasEntry struct {
	// IP address of the host file entry.
	IP string `json:"ip"`

	// Hostnames for the above IP address.
	Hostnames []string `json:"hostnames,omitempty"`
}

//+kubebuilder:object:generate=true

// WorkloadHostAliasSpec defines the desired state of WorkloadHostAlias.
type WorkloadHostAliasSpec struct {
	// Target references the workload to manage.
	Target WorkloadTarget `json:"target"`

	// HostAliases is the list of IP-hostname mappings to inject into the target workload.
	HostAliases []HostAliasEntry `json:"hostAliases"`
}

//+kubebuilder:object:generate=true

// WorkloadHostAliasStatus defines the observed state of WorkloadHostAlias.
type WorkloadHostAliasStatus struct {
	// ObservedGeneration is the last generation the controller processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncTime is when the last successful sync occurred.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Target Kind",type=string,JSONPath=`.spec.target.kind`
//+kubebuilder:printcolumn:name="Target Name",type=string,JSONPath=`.spec.target.name`
//+kubebuilder:printcolumn:name="Target Namespace",type=string,JSONPath=`.spec.target.namespace`
//+kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// WorkloadHostAlias is the Schema for the workloadhostaliases API.
type WorkloadHostAlias struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadHostAliasSpec   `json:"spec,omitempty"`
	Status WorkloadHostAliasStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WorkloadHostAliasList contains a list of WorkloadHostAlias.
type WorkloadHostAliasList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []WorkloadHostAlias `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadHostAlias{}, &WorkloadHostAliasList{})
}
