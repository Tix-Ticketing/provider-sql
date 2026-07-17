/*
Copyright 2020 The Crossplane Authors.
Licensed under the Apache License, Version 2.0 (the "License").
*/

package table

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"

	"github.com/crossplane-contrib/provider-sql/apis/cluster/singlestore/v1alpha1"
	"github.com/crossplane-contrib/provider-sql/pkg/clients/xsql"
)

// mockDB satisfies xsql.DB. Only Scan/Exec are exercised; Query/ExecTx are stubbed.
type mockDB struct {
	MockExec func(ctx context.Context, q xsql.Query) error
	MockScan func(ctx context.Context, q xsql.Query, dest ...interface{}) error
}

func (m mockDB) Exec(ctx context.Context, q xsql.Query) error      { return m.MockExec(ctx, q) }
func (m mockDB) ExecTx(ctx context.Context, ql []xsql.Query) error { return nil }
func (m mockDB) Scan(ctx context.Context, q xsql.Query, dest ...interface{}) error {
	return m.MockScan(ctx, q, dest...)
}
func (m mockDB) Query(ctx context.Context, q xsql.Query) (*sql.Rows, error) { return &sql.Rows{}, nil }
func (m mockDB) GetConnectionDetails(u, p string) managed.ConnectionDetails { return nil }
func (m mockDB) GetServerVersion(ctx context.Context) (int, error)          { return 0, nil }

func tbl(name, db, create, gen string) *v1alpha1.Table {
	t := &v1alpha1.Table{}
	t.SetName(name)
	meta.SetExternalName(t, name)
	t.Spec.ForProvider.Database = db
	t.Spec.ForProvider.CreateStatement = create
	t.Spec.ForProvider.SchemaGeneration = gen
	return t
}

func errCode(n uint16) error { return &mysql.MySQLError{Number: n} }

func TestObserve(t *testing.T) {
	type want struct {
		exists   bool
		upToDate bool
		applied  string
		errNil   bool
	}
	cases := map[string]struct {
		scan func(ctx context.Context, q xsql.Query, dest ...interface{}) error
		want want
	}{
		"NotExists": {
			scan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
				if strings.Contains(q.String, "information_schema.tables") {
					return sql.ErrNoRows
				}
				return nil
			},
			want: want{exists: false, errNil: true},
		},
		"UpToDate": {
			scan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
				switch {
				case strings.Contains(q.String, "information_schema.tables"):
					*(dest[0].(*int)) = 1
					return nil
				case strings.Contains(q.String, "schema_generation"):
					*(dest[0].(*string)) = "sha256:v1"
					return nil
				}
				return nil
			},
			want: want{exists: true, upToDate: true, applied: "sha256:v1", errNil: true},
		},
		"RecreateNeeded": {
			scan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
				switch {
				case strings.Contains(q.String, "information_schema.tables"):
					*(dest[0].(*int)) = 1
					return nil
				case strings.Contains(q.String, "schema_generation"):
					*(dest[0].(*string)) = "sha256:OLD"
					return nil
				}
				return nil
			},
			want: want{exists: true, upToDate: false, applied: "sha256:OLD", errNil: true},
		},
		"AdoptNoMarkerRow": {
			scan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
				if strings.Contains(q.String, "information_schema.tables") {
					*(dest[0].(*int)) = 1
					return nil
				}
				return sql.ErrNoRows // marker row absent
			},
			want: want{exists: true, upToDate: false, applied: "", errNil: true},
		},
		"AdoptMarkerTableMissing": {
			scan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
				if strings.Contains(q.String, "information_schema.tables") {
					*(dest[0].(*int)) = 1
					return nil
				}
				return errCode(errCodeUnknownTable) // 1146
			},
			want: want{exists: true, upToDate: false, applied: "", errNil: true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := external{db: mockDB{MockScan: tc.scan}}
			mg := tbl("allocationtype", "tickets_prod_fo", "CREATE TABLE x", "sha256:v1")
			got, err := e.Observe(context.Background(), mg)
			if tc.want.errNil && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.ResourceExists != tc.want.exists || got.ResourceUpToDate != tc.want.upToDate {
				t.Errorf("Observe = exists:%v upToDate:%v, want exists:%v upToDate:%v", got.ResourceExists, got.ResourceUpToDate, tc.want.exists, tc.want.upToDate)
			}
			if mg.Status.AtProvider.AppliedGeneration != tc.want.applied {
				t.Errorf("applied = %q, want %q", mg.Status.AtProvider.AppliedGeneration, tc.want.applied)
			}
		})
	}
}

// recorder captures executed statements in order.
func recorder() (*[]string, func(ctx context.Context, q xsql.Query) error) {
	var got []string
	return &got, func(_ context.Context, q xsql.Query) error { got = append(got, q.String); return nil }
}

func TestCreate(t *testing.T) {
	got, exec := recorder()
	e := external{db: mockDB{MockExec: exec}}
	mg := tbl("allocationtype", "tickets_prod_fo", "CREATE ROWSTORE REFERENCE TABLE `tickets_prod_fo`.`allocationtype` (...)", "sha256:v1")
	if _, err := e.Create(context.Background(), mg); err != nil {
		t.Fatal(err)
	}
	// createStatement -> ensure marker table -> REPLACE marker, in that order.
	if len(*got) != 3 ||
		!strings.HasPrefix((*got)[0], "CREATE ROWSTORE REFERENCE TABLE") ||
		!strings.Contains((*got)[1], "CREATE ROWSTORE REFERENCE TABLE IF NOT EXISTS") || !strings.Contains((*got)[1], "cdc_table_state") ||
		!strings.HasPrefix((*got)[2], "REPLACE INTO") {
		t.Fatalf("Create order wrong: %#v", *got)
	}
}

func TestUpdateAdopt(t *testing.T) {
	got, exec := recorder()
	// Update re-reads the marker; no marker row -> adopt.
	e := external{db: mockDB{
		MockExec: exec,
		MockScan: func(_ context.Context, q xsql.Query, dest ...interface{}) error { return sql.ErrNoRows },
	}}
	mg := tbl("allocationtype", "tickets_prod_fo", "CREATE TABLE x", "sha256:v1")
	if _, err := e.Update(context.Background(), mg); err != nil {
		t.Fatal(err)
	}
	// adopt = ensure marker + REPLACE, and NO DROP / NO createStatement.
	for _, s := range *got {
		if strings.HasPrefix(s, "DROP TABLE") || s == "CREATE TABLE x" {
			t.Fatalf("adopt must not DROP/CREATE the data table: %#v", *got)
		}
	}
	if len(*got) != 2 || !strings.Contains((*got)[0], "cdc_table_state") || !strings.HasPrefix((*got)[1], "REPLACE INTO") {
		t.Fatalf("adopt order wrong: %#v", *got)
	}
}

func TestUpdateRecreate(t *testing.T) {
	got, exec := recorder()
	// Update re-reads the marker; stale generation present -> recreate.
	e := external{db: mockDB{
		MockExec: exec,
		MockScan: func(_ context.Context, q xsql.Query, dest ...interface{}) error {
			*(dest[0].(*string)) = "sha256:OLD"
			return nil
		},
	}}
	mg := tbl("allocationtype", "tickets_prod_fo", "CREATE TABLE fresh", "sha256:v2")
	if _, err := e.Update(context.Background(), mg); err != nil {
		t.Fatal(err)
	}
	// DROP -> createStatement -> ensure marker -> REPLACE.
	if len(*got) != 4 ||
		!strings.HasPrefix((*got)[0], "DROP TABLE IF EXISTS") ||
		(*got)[1] != "CREATE TABLE fresh" ||
		!strings.Contains((*got)[2], "cdc_table_state") ||
		!strings.HasPrefix((*got)[3], "REPLACE INTO") {
		t.Fatalf("recreate order wrong: %#v", *got)
	}
}

func TestDeleteToleratesMissing(t *testing.T) {
	// DROP -> 1049 (unknown db) tolerated; DELETE marker -> 1146 tolerated.
	call := 0
	e := external{db: mockDB{MockExec: func(_ context.Context, q xsql.Query) error {
		call++
		if strings.HasPrefix(q.String, "DROP TABLE") {
			return errors.Wrap(errCode(errCodeUnknownDatabase), "cannot drop table")
		}
		return errors.Wrap(errCode(errCodeUnknownTable), "cannot delete marker row")
	}}}
	mg := tbl("allocationtype", "tickets_prod_fo", "CREATE TABLE x", "sha256:v1")
	if _, err := e.Delete(context.Background(), mg); err != nil {
		t.Fatalf("Delete should tolerate 1049/1146: %v", err)
	}
	if call != 2 {
		t.Fatalf("expected DROP + DELETE, got %d calls", call)
	}
}
