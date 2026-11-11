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

// A TableSpec defines the desired state of a Table.
type TableSpec struct {
	xpv2.ClusterManagedResourceSpec `json:",inline"`
	ForProvider       TableParameters `json:"forProvider"`
}

// A TableStatus represents the observed state of a Table.
type TableStatus struct {
	xpv2.ManagedResourceStatus `json:",inline"`
	AtProvider          TableObservation `json:"atProvider,omitempty"`
}

// TableParameters define the desired state of a SingleStore table.
//
// The controller treats the DDL as opaque: it executes CreateStatement verbatim
// and never parses or assembles it. Drift is detected solely via
// SchemaGeneration; the live table schema is never compared.
type TableParameters struct {
	// Database is the SingleStore database the table lives in. Used to build the
	// existence check, the DROP, and the location of the cdc_table_state marker.
	// +kubebuilder:validation:MinLength=1
	Database string `json:"database"`

	// CreateStatement is the full, database-qualified CREATE ... TABLE statement,
	// executed verbatim. It must be a single self-contained statement (the client
	// has no transactions) and is produced upstream (the generator, SP2).
	// +kubebuilder:validation:MinLength=1
	CreateStatement string `json:"createStatement"`

	// SchemaGeneration is an opaque content hash of CreateStatement and the sole
	// drift signal: when it changes, the table is dropped and recreated. Compared
	// by equality only.
	// +kubebuilder:validation:MinLength=1
	SchemaGeneration string `json:"schemaGeneration"`
}

// A TableObservation represents the observed state of a SingleStore table.
type TableObservation struct {
	// AppliedGeneration is the schema_generation recorded in the cdc_table_state
	// marker, or empty if no marker row exists yet (an as-yet-unadopted table).
	AppliedGeneration string `json:"appliedGeneration,omitempty"`

	// Ready is true when the table exists and the marker matches SchemaGeneration.
	Ready bool `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true

// A Table represents the declarative state of a SingleStore table.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="DATABASE",type="string",JSONPath=".spec.forProvider.database"
// +kubebuilder:printcolumn:name="APPLIED",type="string",JSONPath=".status.atProvider.appliedGeneration"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,sql}
type Table struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TableSpec   `json:"spec"`
	Status TableStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TableList contains a list of Table
type TableList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Table `json:"items"`
}
