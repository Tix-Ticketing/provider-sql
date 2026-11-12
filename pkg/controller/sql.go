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

package controller

import (
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"

	clustermssql "github.com/crossplane-contrib/provider-sql/pkg/controller/cluster/mssql"
	clustermysql "github.com/crossplane-contrib/provider-sql/pkg/controller/cluster/mysql"
	clusterpostgresql "github.com/crossplane-contrib/provider-sql/pkg/controller/cluster/postgresql"
	clustersinglestore "github.com/crossplane-contrib/provider-sql/pkg/controller/cluster/singlestore"
	namespacedmssql "github.com/crossplane-contrib/provider-sql/pkg/controller/namespaced/mssql"
	namespacedmysql "github.com/crossplane-contrib/provider-sql/pkg/controller/namespaced/mysql"
	namespacedpostgresql "github.com/crossplane-contrib/provider-sql/pkg/controller/namespaced/postgresql"
)

// flavours maps a flavour name to the controller setup functions it owns.
// Gating by flavour lets us ship a singlestore-only package that runs alongside
// the upstream provider-sql without both establishing the same CRDs.
var flavours = map[string][]func(ctrl.Manager, controller.Options) error{
	"mysql":       {clustermysql.Setup, namespacedmysql.Setup},
	"postgresql":  {clusterpostgresql.Setup, namespacedpostgresql.Setup},
	"mssql":       {clustermssql.Setup, namespacedmssql.Setup},
	"singlestore": {clustersinglestore.Setup},
}

// AllFlavours returns every known flavour name.
func AllFlavours() []string {
	return []string{"mysql", "postgresql", "mssql", "singlestore"}
}

// Setup creates controllers for every flavour. Retained for callers that want
// the full provider.
func Setup(mgr ctrl.Manager, l controller.Options) error {
	return SetupSelected(mgr, l, AllFlavours())
}

// SetupSelected creates controllers only for the named flavours. An unknown
// flavour is a hard error so a typo can't silently disable a controller.
func SetupSelected(mgr ctrl.Manager, l controller.Options, names []string) error {
	for _, name := range names {
		setups, ok := flavours[name]
		if !ok {
			return errors.Errorf("unknown sql flavour %q (known: mysql, postgresql, mssql, singlestore)", name)
		}
		for _, setup := range setups {
			if err := setup(mgr, l); err != nil {
				return err
			}
		}
	}
	return nil
}
