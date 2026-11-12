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

package grant

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestParseGrant covers the SingleStore-specific SHOW GRANTS output forms,
// verified against a live SingleStore v8 instance: a leading TRANSFERABLE
// keyword, trailing "IDENTIFIED {BY PASSWORD '<hash>'|WITH authentication_jwt}
// REQUIRE SSL" on the *.* line, and a trailing WITH GRANT OPTION on db lines.
func TestParseGrant(t *testing.T) {
	cases := map[string]struct {
		grant  string
		dbname string
		table  string
		want   []string
	}{
		"PlainDatabaseScope": {
			grant:  "GRANT SELECT, INSERT ON `mydb`.* TO 'user1'@'%'",
			dbname: "`mydb`",
			table:  "*",
			want:   []string{"SELECT", "INSERT"},
		},
		"WithGrantOption": {
			grant:  "GRANT SELECT, INSERT ON `mydb`.* TO 'user1'@'%' WITH GRANT OPTION",
			dbname: "`mydb`",
			table:  "*",
			want:   []string{"SELECT", "INSERT", "GRANT OPTION"},
		},
		// Real *.* USAGE lines from the live instance carry the auth + REQUIRE SSL
		// suffix; when the scope matches, USAGE must still be extracted cleanly.
		"PasswordUserUsageLine": {
			grant:  "GRANT USAGE ON *.* TO 'pw_user'@'%' IDENTIFIED BY PASSWORD '*B33F04204E36ABA9723120C2973D12DC8C5C6EAD' REQUIRE SSL",
			dbname: "*",
			table:  "*",
			want:   []string{"USAGE"},
		},
		"JWTUserUsageLine": {
			grant:  "GRANT USAGE ON *.* TO 'svc-analytics@tixly.com'@'%' IDENTIFIED WITH authentication_jwt REQUIRE SSL",
			dbname: "*",
			table:  "*",
			want:   []string{"USAGE"},
		},
		"DBScopedWithGrantOption": {
			grant:  "GRANT SELECT, INSERT ON `testdb`.* TO 'pw_user'@'%' WITH GRANT OPTION",
			dbname: "`testdb`",
			table:  "*",
			want:   []string{"SELECT", "INSERT", "GRANT OPTION"},
		},
		"TransferablePrefix": {
			grant:  "GRANT TRANSFERABLE SELECT, INSERT, UPDATE ON `mydb`.* TO 'user1'@'%'",
			dbname: "`mydb`",
			table:  "*",
			want:   []string{"SELECT", "INSERT", "UPDATE"},
		},
		"IdentifiedByPasswordSuffix": {
			grant:  "GRANT SHOW METADATA ON *.* TO 'user1'@'%' IDENTIFIED BY PASSWORD '*785FBD495FC22B3620EB1572D2504C03B1278554'",
			dbname: "*",
			table:  "*",
			want:   []string{"SHOW METADATA"},
		},
		"IdentifiedByPasswordRedactedSuffix": {
			grant:  "GRANT SHOW METADATA ON *.* TO 'user1'@'%' IDENTIFIED BY PASSWORD <secret>",
			dbname: "*",
			table:  "*",
			want:   []string{"SHOW METADATA"},
		},
		"TransferableAndGrantOption": {
			grant:  "GRANT TRANSFERABLE SELECT ON `mydb`.* TO 'user1'@'%' WITH GRANT OPTION",
			dbname: "`mydb`",
			table:  "*",
			want:   []string{"SELECT", "GRANT OPTION"},
		},
		"AllPrivilegesGlobal": {
			grant:  "GRANT ALL PRIVILEGES ON *.* TO 'root'@'%'",
			dbname: "*",
			table:  "*",
			want:   []string{"ALL PRIVILEGES"},
		},
		"ColumnLevel": {
			grant:  "GRANT SELECT (`col1`, `col2`) ON `mydb`.`t` TO 'user1'@'%'",
			dbname: "`mydb`",
			table:  "`t`",
			want:   []string{"SELECT (`col1`, `col2`)"},
		},
		"NonMatchingScope": {
			grant:  "GRANT SELECT ON `other`.* TO 'user1'@'%'",
			dbname: "`mydb`",
			table:  "*",
			want:   nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseGrant(tc.grant, tc.dbname, tc.table)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseGrant(...): -want, +got:\n%s", diff)
			}
		})
	}
}
