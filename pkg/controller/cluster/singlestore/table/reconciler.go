/*
Copyright 2020 The Crossplane Authors.
Licensed under the Apache License, Version 2.0 (the "License").
*/

package table

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/v2/pkg/statemetrics"
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

	errObserveTable    = "cannot observe table"
	errReadMarker      = "cannot read cdc_table_state marker"
	errCreateTable     = "cannot create table"
	errEnsureMarker    = "cannot ensure cdc_table_state marker table"
	errWriteMarker     = "cannot write cdc_table_state marker"
	errDropTable       = "cannot drop table"
	errDeleteMarkerRow = "cannot delete cdc_table_state marker row"

	// SingleStore/MySQL server error codes.
	errCodeUnknownTable    = 1146 // ER_NO_SUCH_TABLE: marker table not yet created
	errCodeUnknownDatabase = 1049 // ER_BAD_DB_ERROR: parent database gone on delete

	markerTable    = "cdc_table_state"
	maxConcurrency = 5
)

// Setup adds a controller that reconciles Table managed resources.
func Setup(mgr ctrl.Manager, o xpcontroller.Options) error {
	name := managed.ControllerName(v1alpha1.TableGroupKind)
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
	r := managed.NewReconciler(mgr, resource.ManagedKind(v1alpha1.TableGroupVersionKind), reconcilerOptions...)

	if err := mgr.Add(statemetrics.NewMRStateRecorder(
		mgr.GetClient(), o.Logger, o.MetricOptions.MRStateMetrics,
		&v1alpha1.TableList{}, o.MetricOptions.PollStateMetricInterval,
	)); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1alpha1.Table{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrency}).
		Complete(r)
}

type connector struct {
	kube  client.Client
	track func(ctx context.Context, mg resource.LegacyManaged) error
	newDB func(creds map[string][]byte, tls *string, binlog *bool) xsql.DB
}

var _ managed.TypedExternalConnector[*v1alpha1.Table] = &connector{}

func (c *connector) Connect(ctx context.Context, mg *v1alpha1.Table) (managed.TypedExternalClient[*v1alpha1.Table], error) {
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
	return &external{db: c.newDB(secretData, tlsName, nil), kube: c.kube}, nil
}

type external struct {
	db   xsql.DB
	kube client.Client
}

var _ managed.TypedExternalClient[*v1alpha1.Table] = &external{}

func tableRef(db, name string) string {
	return mysql.QuoteIdentifier(db) + "." + mysql.QuoteIdentifier(name)
}
func markerRef(db string) string {
	return mysql.QuoteIdentifier(db) + "." + mysql.QuoteIdentifier(markerTable)
}

func (c *external) Observe(ctx context.Context, mg *v1alpha1.Table) (managed.ExternalObservation, error) {
	db := mg.Spec.ForProvider.Database
	name := meta.GetExternalName(mg)

	var found int
	err := c.db.Scan(ctx, xsql.Query{
		String:     "SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		Parameters: []interface{}{db, name},
	}, &found)
	if xsql.IsNoRows(err) {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errObserveTable)
	}

	applied, err := c.readMarker(ctx, db, name)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errReadMarker)
	}
	mg.Status.AtProvider.AppliedGeneration = applied
	upToDate := applied != "" && applied == mg.Spec.ForProvider.SchemaGeneration
	mg.Status.AtProvider.Ready = upToDate
	mg.SetConditions(xpv1.Available())
	return managed.ExternalObservation{ResourceExists: true, ResourceUpToDate: upToDate}, nil
}

// readMarker returns the applied generation, or "" if there is no marker row or
// the marker table itself does not exist yet (1146).
func (c *external) readMarker(ctx context.Context, db, name string) (string, error) {
	var gen string
	err := c.db.Scan(ctx, xsql.Query{
		String:     fmt.Sprintf("SELECT schema_generation FROM %s WHERE target_name = ?", markerRef(db)),
		Parameters: []interface{}{name},
	}, &gen)
	if xsql.IsNoRows(err) || mysql.IsErrorCode(err, errCodeUnknownTable) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return gen, nil
}

func (c *external) writeMarker(ctx context.Context, db, name, generation string) error {
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{
		Query: "CREATE ROWSTORE REFERENCE TABLE IF NOT EXISTS " + markerRef(db) +
			" (target_name VARCHAR(255) NOT NULL PRIMARY KEY, schema_generation VARCHAR(80) NOT NULL, ready_at DATETIME NOT NULL)",
		ErrorValue: errEnsureMarker,
	}); err != nil {
		return err
	}
	return mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{
		Query:      "REPLACE INTO " + markerRef(db) + " VALUES (" + mysql.QuoteValue(name) + ", " + mysql.QuoteValue(generation) + ", NOW())",
		ErrorValue: errWriteMarker,
	})
}

func (c *external) Create(ctx context.Context, mg *v1alpha1.Table) (managed.ExternalCreation, error) {
	mg.SetConditions(xpv1.Creating())
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: mg.Spec.ForProvider.CreateStatement, ErrorValue: errCreateTable}); err != nil {
		return managed.ExternalCreation{}, err
	}
	if err := c.writeMarker(ctx, mg.Spec.ForProvider.Database, meta.GetExternalName(mg), mg.Spec.ForProvider.SchemaGeneration); err != nil {
		return managed.ExternalCreation{}, err
	}
	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg *v1alpha1.Table) (managed.ExternalUpdate, error) {
	db := mg.Spec.ForProvider.Database
	name := meta.GetExternalName(mg)

	// Re-read the marker to branch. Do NOT rely on Status.AtProvider stashed
	// during Observe: crossplane-runtime's AddFinalizer issues a client.Update
	// between Observe and Update on a fresh MR's first reconcile, which resets
	// in-memory status to the persisted (empty) value and would mis-route a
	// recreate to an adopt. Empty marker = adopt an existing table (write marker,
	// no DDL); present-but-different = recreate.
	applied, err := c.readMarker(ctx, db, name)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errReadMarker)
	}
	if applied != "" {
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: "DROP TABLE IF EXISTS " + tableRef(db, name), ErrorValue: errDropTable}); err != nil {
			return managed.ExternalUpdate{}, err
		}
		if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: mg.Spec.ForProvider.CreateStatement, ErrorValue: errCreateTable}); err != nil {
			return managed.ExternalUpdate{}, err
		}
	}
	if err := c.writeMarker(ctx, db, name, mg.Spec.ForProvider.SchemaGeneration); err != nil {
		return managed.ExternalUpdate{}, err
	}
	return managed.ExternalUpdate{}, nil
}

func (c *external) Delete(ctx context.Context, mg *v1alpha1.Table) (managed.ExternalDelete, error) {
	mg.SetConditions(xpv1.Deleting())
	db := mg.Spec.ForProvider.Database
	name := meta.GetExternalName(mg)

	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{Query: "DROP TABLE IF EXISTS " + tableRef(db, name), ErrorValue: errDropTable}); err != nil {
		if !mysql.IsErrorCode(err, errCodeUnknownDatabase) {
			return managed.ExternalDelete{}, err
		}
	}
	if err := mysql.ExecWrapper(ctx, c.db, mysql.ExecQuery{
		Query:      "DELETE FROM " + markerRef(db) + " WHERE target_name = " + mysql.QuoteValue(name),
		ErrorValue: errDeleteMarkerRow,
	}); err != nil {
		if !mysql.IsErrorCode(err, errCodeUnknownTable) && !mysql.IsErrorCode(err, errCodeUnknownDatabase) {
			return managed.ExternalDelete{}, err
		}
	}
	return managed.ExternalDelete{}, nil
}

func (c *external) Disconnect(ctx context.Context) error { return nil }
