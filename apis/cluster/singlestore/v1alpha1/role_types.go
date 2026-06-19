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

// A RoleSpec defines the desired state of a Role.
type RoleSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       RoleParameters `json:"forProvider"`
}

// RolePrivilege is a set of privileges granted to a role on one scope
// (database.table). It mirrors the GRANT ... ON <db>.<table> TO ROLE form.
type RolePrivilege struct {
	// Privileges to grant on this scope.
	Privileges GrantPrivileges `json:"privileges"`

	// Database this privilege block applies to. Defaults to "*" (all databases).
	// +optional
	Database *string `json:"database,omitempty" default:"*"`

	// Table this privilege block applies to. Defaults to "*" (all tables).
	// +optional
	Table *string `json:"table,omitempty" default:"*"`
}

// RoleParameters define the desired state of a SingleStore role. A role is a
// named bundle of privileges that is attached to Groups (not directly to
// users); users gain a role's privileges by being members of a Group that the
// role is granted to. See the Group resource.
type RoleParameters struct {
	// Privileges granted to this role, one block per database.table scope.
	// SingleStore has no CREATE ROLE IF NOT EXISTS, so the controller creates
	// the role then reconciles its privileges via GRANT/REVOKE ... TO/FROM ROLE.
	// +optional
	Privileges []RolePrivilege `json:"privileges,omitempty"`
}

// A RoleStatus represents the observed state of a Role.
type RoleStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          RoleObservation `json:"atProvider,omitempty"`
}

// A RoleObservation represents the observed state of a SingleStore role.
type RoleObservation struct{}

// +kubebuilder:object:root=true

// A Role represents the declarative state of a SingleStore role.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,sql}
type Role struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RoleSpec   `json:"spec"`
	Status RoleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RoleList contains a list of Role
type RoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Role `json:"items"`
}
