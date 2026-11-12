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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Package type metadata.
const (
	Group   = "singlestore.sql.crossplane.io"
	Version = "v1alpha1"
)

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: Group, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = runtime.NewSchemeBuilder()
)

// ProviderConfig type metadata.
var (
	ProviderConfigKind             = reflect.TypeOf(ProviderConfig{}).Name()
	ProviderConfigGroupKind        = schema.GroupKind{Group: Group, Kind: ProviderConfigKind}.String()
	ProviderConfigKindAPIVersion   = ProviderConfigKind + "." + SchemeGroupVersion.String()
	ProviderConfigGroupVersionKind = SchemeGroupVersion.WithKind(ProviderConfigKind)
)

// ProviderConfigUsage type metadata.
var (
	ProviderConfigUsageKind             = reflect.TypeOf(ProviderConfigUsage{}).Name()
	ProviderConfigUsageGroupKind        = schema.GroupKind{Group: Group, Kind: ProviderConfigUsageKind}.String()
	ProviderConfigUsageKindAPIVersion   = ProviderConfigUsageKind + "." + SchemeGroupVersion.String()
	ProviderConfigUsageGroupVersionKind = SchemeGroupVersion.WithKind(ProviderConfigUsageKind)

	ProviderConfigUsageListKind             = reflect.TypeOf(ProviderConfigUsageList{}).Name()
	ProviderConfigUsageListGroupKind        = schema.GroupKind{Group: Group, Kind: ProviderConfigUsageListKind}.String()
	ProviderConfigUsageListKindAPIVersion   = ProviderConfigUsageListKind + "." + SchemeGroupVersion.String()
	ProviderConfigUsageListGroupVersionKind = SchemeGroupVersion.WithKind(ProviderConfigUsageListKind)
)

// User type metadata.
var (
	UserKind             = reflect.TypeOf(User{}).Name()
	UserGroupKind        = schema.GroupKind{Group: Group, Kind: UserKind}.String()
	UserKindAPIVersion   = UserKind + "." + SchemeGroupVersion.String()
	UserGroupVersionKind = SchemeGroupVersion.WithKind(UserKind)
)

// Grant type metadata.
var (
	GrantKind             = reflect.TypeOf(Grant{}).Name()
	GrantGroupKind        = schema.GroupKind{Group: Group, Kind: GrantKind}.String()
	GrantKindAPIVersion   = GrantKind + "." + SchemeGroupVersion.String()
	GrantGroupVersionKind = SchemeGroupVersion.WithKind(GrantKind)
)

// Role type metadata.
var (
	RoleKind             = reflect.TypeOf(Role{}).Name()
	RoleGroupKind        = schema.GroupKind{Group: Group, Kind: RoleKind}.String()
	RoleKindAPIVersion   = RoleKind + "." + SchemeGroupVersion.String()
	RoleGroupVersionKind = SchemeGroupVersion.WithKind(RoleKind)
)

// RBACGroup type metadata.
var (
	RBACGroupKind             = reflect.TypeOf(RBACGroup{}).Name()
	RBACGroupGroupKind        = schema.GroupKind{Group: Group, Kind: RBACGroupKind}.String()
	RBACGroupKindAPIVersion   = RBACGroupKind + "." + SchemeGroupVersion.String()
	RBACGroupGroupVersionKind = SchemeGroupVersion.WithKind(RBACGroupKind)
)

// Table type metadata.
var (
	TableKind             = reflect.TypeOf(Table{}).Name()
	TableGroupKind        = schema.GroupKind{Group: Group, Kind: TableKind}.String()
	TableKindAPIVersion   = TableKind + "." + SchemeGroupVersion.String()
	TableGroupVersionKind = SchemeGroupVersion.WithKind(TableKind)
)

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ProviderConfig{}, &ProviderConfigList{})
		s.AddKnownTypes(SchemeGroupVersion, &ProviderConfigUsage{}, &ProviderConfigUsageList{})
		s.AddKnownTypes(SchemeGroupVersion, &User{}, &UserList{})
		s.AddKnownTypes(SchemeGroupVersion, &Grant{}, &GrantList{})
		s.AddKnownTypes(SchemeGroupVersion, &Role{}, &RoleList{})
		s.AddKnownTypes(SchemeGroupVersion, &RBACGroup{}, &RBACGroupList{})
		s.AddKnownTypes(SchemeGroupVersion, &Table{}, &TableList{})
		metav1.AddToGroupVersion(s, SchemeGroupVersion)

		return nil
	})
}
