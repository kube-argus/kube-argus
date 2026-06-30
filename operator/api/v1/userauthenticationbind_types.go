/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Membership represents a single group the user is bound to.
type Membership struct {
	// gid is the stable group identifier (e.g. "group/g12312312").
	// +required
	// +kubebuilder:validation:MinLength=1
	Gid string `json:"gid"`

	// name is the human-readable group name (e.g. "engineering").
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// domain is the domain that owns the group.
	// +required
	// +kubebuilder:validation:MinLength=1
	Domain string `json:"domain"`
}

// UserAuthenticationBindSpec defines the desired state of UserAuthenticationBind
type UserAuthenticationBindSpec struct {
	// ttl is the lifetime of the authentication bind before it expires
	// (e.g. "12h", "30m"). After the TTL elapses the bind is unbound.
	// +required
	// +kubebuilder:validation:Pattern=`^[0-9]+(ns|us|µs|ms|s|m|h)$`
	TTL string `json:"ttl"`

	// domain is the domain the user authenticates against.
	// +required
	// +kubebuilder:validation:MinLength=1
	Domain string `json:"domain"`

	// user is the user being bound (e.g. "admin").
	// +required
	// +kubebuilder:validation:MinLength=1
	User string `json:"user"`

	// memberships are the group memberships granted to the user by this bind.
	// +optional
	// +listType=atomic
	Memberships []Membership `json:"memberships,omitempty"`
}

// BindPhase enumerates the lifecycle states of a service bind.
// +kubebuilder:validation:Enum=pending;binding;binded;unbound;failed
type BindPhase string

const (
	// BindPending means the bind has been accepted but not yet applied.
	BindPending BindPhase = "pending"
	// BindBinding means the RBAC bindings are actively being synced (transient).
	BindBinding BindPhase = "binding"
	// BindBinded means the user/groups are bound (RBAC applied).
	BindBinded BindPhase = "binded"
	// BindUnbound means the bind has expired or been removed.
	BindUnbound BindPhase = "unbound"
	// BindFailed means the bind could not be applied.
	BindFailed BindPhase = "failed"
)

// ServiceBind holds the observed state of the external bind.
type ServiceBind struct {
	// ref is the external reference returned by the binding service.
	// +optional
	Ref string `json:"ref,omitempty"`

	// status is the current phase of the service bind.
	// +optional
	Status BindPhase `json:"status,omitempty"`

	// expiresAt is the time at which this bind expires (creation + ttl).
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`
}

// UserAuthenticationBindStatus defines the observed state of UserAuthenticationBind.
type UserAuthenticationBindStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the UserAuthenticationBind resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// sv holds the observed state of the external bind.
	// +optional
	Sv ServiceBind `json:"sv,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.spec.user`
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.domain`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.sv.status`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.sv.expiresAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UserAuthenticationBind is the Schema for the userauthenticationbinds API
type UserAuthenticationBind struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of UserAuthenticationBind
	// +required
	Spec UserAuthenticationBindSpec `json:"spec"`

	// status defines the observed state of UserAuthenticationBind
	// +optional
	Status UserAuthenticationBindStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// UserAuthenticationBindList contains a list of UserAuthenticationBind
type UserAuthenticationBindList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []UserAuthenticationBind `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &UserAuthenticationBind{}, &UserAuthenticationBindList{})
		return nil
	})
}
