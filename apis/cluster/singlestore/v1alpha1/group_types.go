/*
Copyright 2020 The Crossplane Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// An RBACGroupSpec defines the desired state of an RBACGroup.
type RBACGroupSpec struct {
	xpv2.ClusterManagedResourceSpec `json:",inline"`
	ForProvider       RBACGroupParameters `json:"forProvider"`
}

// RBACGroupParameters define the desired state of a SingleStore RBAC group. A
// group is the join point of SingleStore RBAC: roles are granted to groups
// (GRANT ROLE ... TO <group>) and users are made members of groups
// (GRANT GROUP <group> TO <user>). Users get the union of the privileges of the
// roles attached to the groups they belong to.
type RBACGroupParameters struct {
	// Roles attached to this group, by role name (the Role's external name).
	// Reconciled via GRANT ROLE / REVOKE ROLE ... TO/FROM the group.
	// +optional
	Roles []string `json:"roles,omitempty"`

	// Users that are members of this group, by user name (without @host).
	// Reconciled via GRANT GROUP / REVOKE GROUP ... TO/FROM each user.
	// +optional
	Users []string `json:"users,omitempty"`
}

// An RBACGroupStatus represents the observed state of an RBACGroup.
type RBACGroupStatus struct {
	xpv2.ManagedResourceStatus `json:",inline"`
	AtProvider          RBACGroupObservation `json:"atProvider,omitempty"`
}

// An RBACGroupObservation represents the observed state of a SingleStore group.
type RBACGroupObservation struct {
	// Roles currently attached to the group.
	Roles []string `json:"roles,omitempty"`
	// Users currently members of the group.
	Users []string `json:"users,omitempty"`
}

// +kubebuilder:object:root=true

// An RBACGroup represents the declarative state of a SingleStore RBAC group
// (the join point where roles attach and users become members). Named RBACGroup
// (not Group) because "group" is overloaded; this is specifically a SingleStore
// privilege group.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,sql}
type RBACGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RBACGroupSpec   `json:"spec"`
	Status RBACGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RBACGroupList contains a list of RBACGroup
type RBACGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RBACGroup `json:"items"`
}
