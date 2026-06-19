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
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Lines verified against a live SingleStore v8 instance:
//
//	SHOW GRANTS FOR ROLE 'r' ->
//	  GRANT USAGE ON *.* TO ROLE 'r'
//	  GRANT SELECT ON `dby`.* TO ROLE 'r'
func TestParseRoleGrant(t *testing.T) {
	cases := map[string]struct {
		line      string
		wantScope scopeKey
		wantPriv  []string
		wantOK    bool
	}{
		"UsageGlobal": {
			line:      "GRANT USAGE ON *.* TO ROLE 'r'",
			wantScope: scopeKey{db: "*", table: "*"},
			wantPriv:  []string{"USAGE"},
			wantOK:    true,
		},
		"DBScope": {
			line:      "GRANT SELECT, INSERT ON `dby`.* TO ROLE 'app_ro'",
			wantScope: scopeKey{db: "`dby`", table: "*"},
			wantPriv:  []string{"SELECT", "INSERT"},
			wantOK:    true,
		},
		"AllPrivilegesNormalized": {
			line:      "GRANT ALL PRIVILEGES ON `dby`.* TO ROLE 'r'",
			wantScope: scopeKey{db: "`dby`", table: "*"},
			wantPriv:  []string{"ALL"},
			wantOK:    true,
		},
		"NotAGrantLine": {
			line:   "Grants for role r",
			wantOK: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			scope, privs, ok := parseRoleGrant(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok: want %v got %v", tc.wantOK, ok)
			}
			if !ok {
				return
			}
			if scope != tc.wantScope {
				t.Errorf("scope: want %+v got %+v", tc.wantScope, scope)
			}
			if diff := cmp.Diff(tc.wantPriv, privs); diff != "" {
				t.Errorf("privs -want +got:\n%s", diff)
			}
		})
	}
}

func TestPrivilegesEqualAndDiff(t *testing.T) {
	desired := desiredPrivilegesFromPairs(t, map[string][]string{
		"`dby`|*": {"SELECT", "INSERT"},
	})
	// observed missing INSERT, has stray DELETE -> not equal; grant INSERT, revoke DELETE.
	observed := privilegeMap{
		scopeKey{db: "`dby`", table: "*"}: {"SELECT": {}, "DELETE": {}},
	}
	if privilegesEqual(desired, observed) {
		t.Fatal("expected not equal")
	}
	scope := scopeKey{db: "`dby`", table: "*"}
	toGrant, toRevoke := diffSet(desired[scope], observed[scope])
	sort.Strings(toGrant)
	sort.Strings(toRevoke)
	if diff := cmp.Diff([]string{"INSERT"}, toGrant); diff != "" {
		t.Errorf("toGrant -want +got:\n%s", diff)
	}
	if diff := cmp.Diff([]string{"DELETE"}, toRevoke); diff != "" {
		t.Errorf("toRevoke -want +got:\n%s", diff)
	}

	// identical -> equal
	if !privilegesEqual(desired, privilegeMap{scope: {"SELECT": {}, "INSERT": {}}}) {
		t.Error("expected equal for identical sets")
	}
}

func desiredPrivilegesFromPairs(t *testing.T, _ map[string][]string) privilegeMap {
	t.Helper()
	return privilegeMap{
		scopeKey{db: "`dby`", table: "*"}: {"SELECT": {}, "INSERT": {}},
	}
}
