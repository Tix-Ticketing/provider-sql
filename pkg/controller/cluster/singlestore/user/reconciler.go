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

package user

import (
	"context"
	"fmt"

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
	"github.com/crossplane/crossplane-runtime/v2/pkg/password"
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

	errObserveUser             = "cannot observe user"
	errCreateUser              = "cannot create user"
	errDropUser                = "cannot drop user"
	errUpdateUser              = "cannot update user"
	errGetPasswordSecretFailed = "cannot get password secret"

	errCodeNoSuchGrant = 1141

	maxConcurrency = 5
)

// Setup adds a controller that reconciles User managed resources.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	name := managed.ControllerName(v1alpha1.UserGroupKind)
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
		resource.ManagedKind(v1alpha1.UserGroupVersionKind),
		reconcilerOptions...,
	)
	if err := mgr.Add(statemetrics.NewMRStateRecorder(
		mgr.GetClient(), o.Logger, o.MetricOptions.MRStateMetrics,
		&v1alpha1.UserList{}, o.MetricOptions.PollStateMetricInterval,
	)); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.User{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrency,
		}).
		Complete(r)
}

type connector struct {
	kube  client.Client
	track func(ctx context.Context, mg resource.LegacyManaged) error
	newDB func(creds map[string][]byte, tls *string, binlog *bool) xsql.DB
}

var _ managed.TypedExternalConnector[*v1alpha1.User] = &connector{}

func (c *connector) Connect(ctx context.Context, mg *v1alpha1.User) (managed.TypedExternalClient[*v1alpha1.User], error) {
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
	return &external{
		db:   c.newDB(secretData, tlsName, mg.Spec.ForProvider.BinLog),
		kube: c.kube,
	}, nil
}

type external struct {
	db   xsql.DB
	kube client.Client
}

var _ managed.TypedExternalClient[*v1alpha1.User] = &external{}

func (c *external) userExists(ctx context.Context, username, host string) (bool, error) {
	// SingleStore does not expose the resource-limit columns of mysql.user, so
	// existence is probed via SHOW GRANTS, which returns error 1141 when the
	// user (and therefore any grant) does not exist.
	query := fmt.Sprintf("SHOW GRANTS FOR %s@%s", mysql.QuoteValue(username), mysql.QuoteValue(host))
	rows, err := c.db.Query(ctx, xsql.Query{String: query})
	if err != nil {
		var myErr *mysqldriver.MySQLError
		if errors.As(err, &myErr) && myErr.Number == errCodeNoSuchGrant {
			return false, nil
		}
		return false, errors.Wrap(err, errObserveUser)
	}
	defer rows.Close() //nolint:errcheck
	return true, rows.Err()
}

func (c *external) Observe(ctx context.Context, mg *v1alpha1.User) (managed.ExternalObservation, error) {
	username, host := mysql.SplitUserHost(meta.GetExternalName(mg))

	exists, err := c.userExists(ctx, username, host)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	if !exists {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}

	// JWT users carry no password, so they are always up to date once created.
	// Password users are up to date unless the referenced password changed.
	pwdChanged := false
	if !isJWT(mg) {
		if _, pwdChanged, err = c.getPassword(ctx, mg); err != nil {
			return managed.ExternalObservation{}, err
		}
	}

	mg.SetConditions(xpv1.Available())

	return managed.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: !pwdChanged,
	}, nil
}

// identifiedClause returns the IDENTIFIED ... fragment for a CREATE USER
// statement, plus any password to persist as a connection detail.
// JWT users use IDENTIFIED WITH authentication_jwt and store no password;
// password users use IDENTIFIED BY (auto-generating a password if none is set).
func (c *external) identifiedClause(ctx context.Context, mg *v1alpha1.User) (clause string, generatedPassword string, err error) {
	if isJWT(mg) {
		return " IDENTIFIED WITH authentication_jwt", "", nil
	}

	pw, _, err := c.getPassword(ctx, mg)
	if err != nil {
		return "", "", err
	}
	if pw == "" {
		if pw, err = password.Generate(); err != nil {
			return "", "", err
		}
	}
	return fmt.Sprintf(" IDENTIFIED BY %s", mysql.QuoteValue(pw)), pw, nil
}

func (c *external) Create(ctx context.Context, mg *v1alpha1.User) (managed.ExternalCreation, error) {
	mg.SetConditions(xpv1.Creating())

	username, host := mysql.SplitUserHost(meta.GetExternalName(mg))

	identified, pw, err := c.identifiedClause(ctx, mg)
	if err != nil {
		return managed.ExternalCreation{}, err
	}

	query := fmt.Sprintf("CREATE USER %s@%s%s",
		mysql.QuoteValue(username),
		mysql.QuoteValue(host),
		identified,
	)
	// REQUIRE SSL is mandatory for JWT users (SingleStore rejects the CREATE
	// otherwise); for password users it is opt-in via RequireSSL.
	if isJWT(mg) || requireSSL(mg) {
		query += " REQUIRE SSL"
	}

	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: query, ErrorValue: errCreateUser}); err != nil {
		return managed.ExternalCreation{}, err
	}

	if pw == "" {
		return managed.ExternalCreation{}, nil
	}
	return managed.ExternalCreation{
		ConnectionDetails: c.db.GetConnectionDetails(username, pw),
	}, nil
}

func (c *external) Update(ctx context.Context, mg *v1alpha1.User) (managed.ExternalUpdate, error) {
	// Only password users have a mutable secret to reconcile. JWT users are
	// immutable post-create in this controller.
	if isJWT(mg) {
		return managed.ExternalUpdate{}, nil
	}

	username, host := mysql.SplitUserHost(meta.GetExternalName(mg))

	pw, pwchanged, err := c.getPassword(ctx, mg)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	if !pwchanged {
		return managed.ExternalUpdate{}, nil
	}

	query := fmt.Sprintf("ALTER USER %s@%s IDENTIFIED BY %s",
		mysql.QuoteValue(username), mysql.QuoteValue(host), mysql.QuoteValue(pw))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: query, ErrorValue: errUpdateUser}); err != nil {
		return managed.ExternalUpdate{}, err
	}

	return managed.ExternalUpdate{ConnectionDetails: c.db.GetConnectionDetails(username, pw)}, nil
}

func (c *external) Delete(ctx context.Context, mg *v1alpha1.User) (managed.ExternalDelete, error) {
	mg.SetConditions(xpv1.Deleting())

	username, host := mysql.SplitUserHost(meta.GetExternalName(mg))

	query := fmt.Sprintf("DROP USER IF EXISTS %s@%s", mysql.QuoteValue(username), mysql.QuoteValue(host))
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: query, ErrorValue: errDropUser}); err != nil {
		return managed.ExternalDelete{}, err
	}

	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error {
	return nil
}

func requireSSL(mg *v1alpha1.User) bool {
	r := mg.Spec.ForProvider.RequireSSL
	return r != nil && *r
}
