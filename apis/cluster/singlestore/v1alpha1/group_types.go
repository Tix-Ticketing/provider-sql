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

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
)

// A GroupSpec defines the desired state of a Group.
type GroupSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       GroupParameters `json:"forProvider"`
}

// GroupParameters define the desired state of a SingleStore group. A group is
// the join point of SingleStore RBAC: roles are granted to groups
// (GRANT ROLE ... TO <group>) and users are made members of groups
// (GRANT GROUP <group> TO <user>). Users get the union of the privileges of the
// roles attached to the groups they belong to.
type GroupParameters struct {
	// Roles attached to this group (by role name). Reconciled via
	// GRANT ROLE / REVOKE ROLE ... TO/FROM the group.
	// +optional
	// +crossplane:generate:reference:type=Role
	Roles []string `json:"roles,omitempty"`

	// RolesRefs resolves Roles to attach to this group.
	// +optional
	RolesRefs []xpv1.Reference `json:"rolesRefs,omitempty"`

	// RolesSelector selects Roles to attach to this group.
	// +optional
	RolesSelector *xpv1.Selector `json:"rolesSelector,omitempty"`

	// Users that are members of this group (by user name, without @host).
	// Reconciled via GRANT GROUP / REVOKE GROUP ... TO/FROM each user.
	// +optional
	// +crossplane:generate:reference:type=User
	Users []string `json:"users,omitempty"`

	// UsersRefs resolves Users to add as members of this group.
	// +optional
	UsersRefs []xpv1.Reference `json:"usersRefs,omitempty"`

	// UsersSelector selects Users to add as members of this group.
	// +optional
	UsersSelector *xpv1.Selector `json:"usersSelector,omitempty"`
}

// A GroupStatus represents the observed state of a Group.
type GroupStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          GroupObservation `json:"atProvider,omitempty"`
}

// A GroupObservation represents the observed state of a SingleStore group.
type GroupObservation struct {
	// Roles currently attached to the group.
	Roles []string `json:"roles,omitempty"`
	// Users currently members of the group.
	Users []string `json:"users,omitempty"`
}

// +kubebuilder:object:root=true

// A Group represents the declarative state of a SingleStore group.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,sql}
type Group struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GroupSpec   `json:"spec"`
	Status GroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GroupList contains a list of Group
type GroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Group `json:"items"`
}
