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

package group

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

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

	errCreateGroup  = "cannot create group"
	errDropGroup    = "cannot drop group"
	errAttachRole   = "cannot grant role to group"
	errDetachRole   = "cannot revoke role from group"
	errAddMember    = "cannot add user to group"
	errRemoveMember = "cannot remove user from group"
	errShowRoles    = "cannot show roles for group"
	errShowUsers    = "cannot show users for group"

	errCodeGroupDoesNotExist = 1918
	maxConcurrency           = 5
)

// Setup adds a controller that reconciles Group managed resources.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	name := managed.ControllerName(v1alpha1.SingleStoreGroupGroupKind)
	t := resource.NewLegacyProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{})

	reconcilerOptions := []managed.ReconcilerOption{
		managed.WithTypedExternalConnector(&connector{kube: mgr.GetClient(), track: t.Track, newDB: mysql.New}),
		managed.WithReferenceResolver(managed.NewAPISimpleReferenceResolver(mgr.GetClient())),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
	}
	if o.Features.Enabled(feature.EnableBetaManagementPolicies) {
		reconcilerOptions = append(reconcilerOptions, managed.WithManagementPolicies())
	}
	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.SingleStoreGroupGroupVersionKind),
		reconcilerOptions...,
	)
	if err := mgr.Add(statemetrics.NewMRStateRecorder(
		mgr.GetClient(), o.Logger, o.MetricOptions.MRStateMetrics,
		&v1alpha1.GroupList{}, o.MetricOptions.PollStateMetricInterval,
	)); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.Group{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

type connector struct {
	kube  client.Client
	track func(ctx context.Context, mg resource.LegacyManaged) error
	newDB func(creds map[string][]byte, tls *string, binlog *bool) xsql.DB
}

var _ managed.TypedExternalConnector[*v1alpha1.Group] = &connector{}

func (c *connector) Connect(ctx context.Context, mg *v1alpha1.Group) (managed.TypedExternalClient[*v1alpha1.Group], error) {
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

var _ managed.TypedExternalClient[*v1alpha1.Group] = &external{}

func (c *external) Observe(ctx context.Context, mg *v1alpha1.Group) (managed.ExternalObservation, error) {
	grp := meta.GetExternalName(mg)

	roles, exists, err := c.observedRoles(ctx, grp)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	if !exists {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	users, err := c.observedUsers(ctx, grp)
	if err != nil {
		return managed.ExternalObservation{}, err
	}

	mg.Status.AtProvider.Roles = sortedKeys(roles)
	mg.Status.AtProvider.Users = sortedKeys(users)

	rGrant, rRevoke := diff(set(mg.Spec.ForProvider.Roles), roles)
	uGrant, uRevoke := diff(set(mg.Spec.ForProvider.Users), users)

	mg.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: len(rGrant)+len(rRevoke)+len(uGrant)+len(uRevoke) == 0,
	}, nil
}

func (c *external) Create(ctx context.Context, mg *v1alpha1.Group) (managed.ExternalCreation, error) {
	mg.SetConditions(xpv1.Creating())
	grp := meta.GetExternalName(mg)
	q := fmt.Sprintf("CREATE GROUP %s", mysql.QuoteValue(grp))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errCreateGroup}); err != nil {
		return managed.ExternalCreation{}, err
	}
	if err := c.reconcileEdges(ctx, grp, set(mg.Spec.ForProvider.Roles), set(mg.Spec.ForProvider.Users), nil, nil); err != nil {
		return managed.ExternalCreation{}, err
	}
	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg *v1alpha1.Group) (managed.ExternalUpdate, error) {
	grp := meta.GetExternalName(mg)
	roles, _, err := c.observedRoles(ctx, grp)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	users, err := c.observedUsers(ctx, grp)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	if err := c.reconcileEdges(ctx, grp, set(mg.Spec.ForProvider.Roles), set(mg.Spec.ForProvider.Users), roles, users); err != nil {
		return managed.ExternalUpdate{}, err
	}
	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg *v1alpha1.Group) (managed.ExternalDelete, error) {
	mg.SetConditions(xpv1.Deleting())
	grp := meta.GetExternalName(mg)
	// DROP GROUP automatically removes its role attachments and memberships.
	q := fmt.Sprintf("DROP GROUP %s", mysql.QuoteValue(grp))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errDropGroup}); err != nil {
		return managed.ExternalDelete{}, err
	}
	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error { return nil }

// reconcileEdges grants/revokes role attachments and user memberships to match
// the desired sets. Observed sets may be nil (treated as empty), e.g. on Create.
func (c *external) reconcileEdges(ctx context.Context, grp string, desiredRoles, desiredUsers, observedRoles, observedUsers map[string]struct{}) error {
	rGrant, rRevoke := diff(desiredRoles, observedRoles)
	for _, role := range rRevoke {
		q := fmt.Sprintf("REVOKE ROLE %s FROM %s", mysql.QuoteValue(role), mysql.QuoteValue(grp))
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errDetachRole}); err != nil {
			return err
		}
	}
	for _, role := range rGrant {
		q := fmt.Sprintf("GRANT ROLE %s TO %s", mysql.QuoteValue(role), mysql.QuoteValue(grp))
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errAttachRole}); err != nil {
			return err
		}
	}

	uGrant, uRevoke := diff(desiredUsers, observedUsers)
	for _, user := range uRevoke {
		q := fmt.Sprintf("REVOKE GROUP %s FROM %s", mysql.QuoteValue(grp), mysql.QuoteValue(user))
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errRemoveMember}); err != nil {
			return err
		}
	}
	for _, user := range uGrant {
		q := fmt.Sprintf("GRANT GROUP %s TO %s", mysql.QuoteValue(grp), mysql.QuoteValue(user))
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: q, ErrorValue: errAddMember}); err != nil {
			return err
		}
	}
	return nil
}

// observedRoles returns the roles attached to the group. The bool is false when
// the group does not exist (SHOW ROLES FOR GROUP -> error 1918).
func (c *external) observedRoles(ctx context.Context, grp string) (map[string]struct{}, bool, error) {
	q := fmt.Sprintf("SHOW ROLES FOR GROUP %s", mysql.QuoteValue(grp))
	rows, err := c.db.Query(ctx, xsql.Query{String: q})
	if err != nil {
		var myErr *mysqldriver.MySQLError
		if errors.As(err, &myErr) && myErr.Number == errCodeGroupDoesNotExist {
			return nil, false, nil
		}
		return nil, false, errors.Wrap(err, errShowRoles)
	}
	defer rows.Close() //nolint:errcheck

	out := map[string]struct{}{}
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, false, err
		}
		out[role] = struct{}{}
	}
	return out, true, rows.Err()
}

// observedUsers returns the bare usernames that are members of the group.
// SHOW USERS FOR GROUP returns multiple columns ('user'@'host', Type, ...); we
// read the first column and reduce it to the bare username GRANT GROUP expects.
func (c *external) observedUsers(ctx context.Context, grp string) (map[string]struct{}, error) {
	q := fmt.Sprintf("SHOW USERS FOR GROUP %s", mysql.QuoteValue(grp))
	rows, err := c.db.Query(ctx, xsql.Query{String: q})
	if err != nil {
		return nil, errors.Wrap(err, errShowUsers)
	}
	defer rows.Close() //nolint:errcheck

	cols, err := rows.Columns()
	if err != nil {
		return nil, errors.Wrap(err, errShowUsers)
	}
	cells := make([]sql.RawBytes, len(cols))
	dest := make([]interface{}, len(cols))
	for i := range cells {
		dest[i] = &cells[i]
	}

	out := map[string]struct{}{}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, errors.Wrap(err, errShowUsers)
		}
		out[bareUsername(string(cells[0]))] = struct{}{}
	}
	return out, rows.Err()
}

func set(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, i := range items {
		out[i] = struct{}{}
	}
	return out
}

func diff(desired, observed map[string]struct{}) (toAdd, toRemove []string) {
	for k := range desired {
		if _, ok := observed[k]; !ok {
			toAdd = append(toAdd, k)
		}
	}
	for k := range observed {
		if _, ok := desired[k]; !ok {
			toRemove = append(toRemove, k)
		}
	}
	sort.Strings(toAdd)
	sort.Strings(toRemove)
	return toAdd, toRemove
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
