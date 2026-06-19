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

package role

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/statemetrics"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	xpcontroller "github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"

	"github.com/crossplane-contrib/provider-sql/apis/cluster/singlestore/v1alpha1"
	"github.com/crossplane-contrib/provider-sql/pkg/clients/mysql"

	"github.com/crossplane-contrib/provider-sql/pkg/clients/xsql"
	"github.com/crossplane-contrib/provider-sql/pkg/controller/cluster/singlestore/tls"
)

const (
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errNoSecretRef  = "ProviderConfig does not reference a credentials Secret"
	errGetSecret    = "cannot get credentials Secret"
	errTLSConfig    = "cannot load TLS config"

	errCreateRole   = "cannot create role"
	errDropRole     = "cannot drop role"
	errGrantRole    = "cannot grant role privileges"
	errRevokeRole   = "cannot revoke role privileges"
	errCurrentGrant = "cannot show role grants"

	errCodeRoleDoesNotExist = 1919
	maxConcurrency          = 5
)

// Setup adds a controller that reconciles Role managed resources.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	name := managed.ControllerName(v1alpha1.RoleGroupKind)
	t := resource.NewLegacyProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{})

	reconcilerOptions := []managed.ReconcilerOption{
		managed.WithTypedExternalConnector(&connector{kube: mgr.GetClient(), track: t.Track, newDB: mysql.New}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
	}
	if o.Features.Enabled(feature.EnableBetaManagementPolicies) {
		reconcilerOptions = append(reconcilerOptions, managed.WithManagementPolicies())
	}
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.RoleGroupVersionKind),
		reconcilerOptions...,
	)
	if err := mgr.Add(statemetrics.NewMRStateRecorder(
		mgr.GetClient(), o.Logger, o.MetricOptions.MRStateMetrics,
		&v1alpha1.RoleList{}, o.MetricOptions.PollStateMetricInterval,
	)); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.Role{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

type connector struct {
	kube  client.Client
	track func(ctx context.Context, mg resource.LegacyManaged) error
	newDB func(creds map[string][]byte, tls *string, binlog *bool) xsql.DB
}

var _ managed.TypedExternalConnector[*v1alpha1.Role] = &connector{}

func (c *connector) Connect(ctx context.Context, mg *v1alpha1.Role) (managed.TypedExternalClient[*v1alpha1.Role], error) {
	if err := c.track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}
	providerConfigName := mg.GetProviderConfigReference().Name
	pc := &v1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: providerConfigName}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}
	ref := pc.Spec.Credentials.ConnectionSecretRef
	if ref == nil {
		return nil, errors.New(errNoSecretRef)
	}
	s := &corev1.Secret{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, s); err != nil {
		return nil, errors.Wrap(err, errGetSecret)
	}
	tlsName, err := tls.LoadConfig(ctx, c.kube, providerConfigName, pc.Spec.TLS, pc.Spec.TLSConfig)
	if err != nil {
		return nil, errors.Wrap(err, errTLSConfig)
	}
	secretData := xsql.RemapCredentialKeys(s.Data, pc.Spec.Credentials.SecretKeyMapping.ToMap())
	return &external{db: c.newDB(secretData, tlsName, nil)}, nil
}

type external struct{ db xsql.DB }

var _ managed.TypedExternalClient[*v1alpha1.Role] = &external{}

func (c *external) Observe(ctx context.Context, mg *v1alpha1.Role) (managed.ExternalObservation, error) {
	role := meta.GetExternalName(mg)

	observed, exists, err := c.observedPrivileges(ctx, role)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	if !exists {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}

	desired := desiredPrivileges(mg.Spec.ForProvider.Privileges)

	mg.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: privilegesEqual(desired, observed),
	}, nil
}

func (c *external) Create(ctx context.Context, mg *v1alpha1.Role) (managed.ExternalCreation, error) {
	mg.SetConditions(xpv1.Creating())
	role := meta.GetExternalName(mg)

	q := fmt.Sprintf("CREATE ROLE %s", mysql.QuoteValue(role))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errCreateRole}); err != nil {
		return managed.ExternalCreation{}, err
	}
	for scope, privs := range desiredPrivileges(mg.Spec.ForProvider.Privileges) {
		if err := c.grant(ctx, role, scope, setToSorted(privs)); err != nil {
			return managed.ExternalCreation{}, err
		}
	}
	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg *v1alpha1.Role) (managed.ExternalUpdate, error) {
	role := meta.GetExternalName(mg)

	observed, _, err := c.observedPrivileges(ctx, role)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	desired := desiredPrivileges(mg.Spec.ForProvider.Privileges)

	for scope := range allScopes(desired, observed) {
		toGrant, toRevoke := diffSet(desired[scope], observed[scope])
		if len(toRevoke) > 0 {
			if err := c.revoke(ctx, role, scope, toRevoke); err != nil {
				return managed.ExternalUpdate{}, err
			}
		}
		if len(toGrant) > 0 {
			if err := c.grant(ctx, role, scope, toGrant); err != nil {
				return managed.ExternalUpdate{}, err
			}
		}
	}
	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg *v1alpha1.Role) (managed.ExternalDelete, error) {
	mg.SetConditions(xpv1.Deleting())
	role := meta.GetExternalName(mg)
	q := fmt.Sprintf("DROP ROLE IF EXISTS %s", mysql.QuoteValue(role))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errDropRole}); err != nil {
		return managed.ExternalDelete{}, err
	}
	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error { return nil }

func (c *external) grant(ctx context.Context, role string, scope scopeKey, privs []string) error {
	q := fmt.Sprintf("GRANT %s ON %s.%s TO ROLE %s",
		strings.Join(privs, ", "), scope.db, scope.table, mysql.QuoteValue(role))
	return mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errGrantRole})
}

func (c *external) revoke(ctx context.Context, role string, scope scopeKey, privs []string) error {
	q := fmt.Sprintf("REVOKE %s ON %s.%s FROM ROLE %s",
		strings.Join(privs, ", "), scope.db, scope.table, mysql.QuoteValue(role))
	return mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errRevokeRole})
}

func (c *external) observedPrivileges(ctx context.Context, role string) (privilegeMap, bool, error) {
	q := fmt.Sprintf("SHOW GRANTS FOR ROLE %s", mysql.QuoteValue(role))
	rows, err := c.db.Query(ctx, xsql.Query{String: q})
	if err != nil {
		var myErr *mysqldriver.MySQLError
		if errors.As(err, &myErr) && myErr.Number == errCodeRoleDoesNotExist {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, errCurrentGrant)
	}
	defer rows.Close() //nolint:errcheck

	out := privilegeMap{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, false, err
		}
		scope, privs, ok := parseRoleGrant(line)
		if !ok {
			continue
		}
		for _, p := range privs {
			if p == "USAGE" {
				continue
			}
			if out[scope] == nil {
				out[scope] = map[string]struct{}{}
			}
			out[scope][p] = struct{}{}
		}
	}
	return out, true, rows.Err()
}

func setToSorted(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
