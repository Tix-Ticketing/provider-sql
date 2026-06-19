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

// AuthPlugin selects how a SingleStore user authenticates.
type AuthPlugin string

const (
	// AuthPluginPassword creates the user with a native password
	// (CREATE USER ... IDENTIFIED BY '...'). This is the default.
	AuthPluginPassword AuthPlugin = "password"

	// AuthPluginJWT creates a JWT user
	// (CREATE USER ... IDENTIFIED WITH authentication_jwt REQUIRE SSL). No
	// password is stored; the user authenticates by presenting a signed JWT
	// whose resolved username exactly matches this user name (validated against
	// the workspace JWKS endpoint). REQUIRE SSL is mandatory for JWT users and is
	// always emitted. JWT user names may be up to 320 chars (vs 32 for native),
	// so they can be full email addresses. Requires SingleStore v7.8.3+.
	// See https://docs.singlestore.com/cloud/security/database-access/authenticate-via-jwt/
	AuthPluginJWT AuthPlugin = "jwt"
)

// A UserSpec defines the desired state of a User.
type UserSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       UserParameters `json:"forProvider"`
}

// A UserStatus represents the observed state of a User.
type UserStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          UserObservation `json:"atProvider,omitempty"`
}

// UserParameters define the desired state of a SingleStore user instance.
type UserParameters struct {
	// AuthPlugin selects how the user authenticates. "password" (default)
	// creates the user with IDENTIFIED BY a password. "jwt" creates the user
	// with IDENTIFIED WITH authentication_jwt REQUIRE SSL and stores no password;
	// the user authenticates by presenting a signed JWT whose resolved username
	// matches this user name (validated against the workspace JWKS endpoint).
	// +optional
	// +kubebuilder:validation:Enum=password;jwt
	// +kubebuilder:default=password
	AuthPlugin *AuthPlugin `json:"authPlugin,omitempty"`

	// PasswordSecretRef references the secret that contains the password used
	// for this user. Only used when AuthPlugin is "password". If no reference
	// is given for a password user, a password will be auto-generated.
	// +optional
	PasswordSecretRef *xpv1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

	// RequireSSL forces this user to connect over SSL/TLS (REQUIRE SSL).
	// JWT users always get REQUIRE SSL (it is mandatory) regardless of this
	// field. For password users it is optional and defaults to false.
	// +optional
	RequireSSL *bool `json:"requireSSL,omitempty"`

	// BinLog defines whether the create, delete, update operations of this user are propagated to replicas. Defaults to true
	// +optional
	BinLog *bool `json:"binlog,omitempty"`
}

// A UserObservation represents the observed state of a SingleStore user.
type UserObservation struct{}

// +kubebuilder:object:root=true

// A User represents the declarative state of a SingleStore user.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="AUTH",type="string",JSONPath=".spec.forProvider.authPlugin"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,sql}
type User struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSpec   `json:"spec"`
	Status UserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UserList contains a list of User
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []User `json:"items"`
}
