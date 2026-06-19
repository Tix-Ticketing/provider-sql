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
	"regexp"
	"strings"

	"github.com/crossplane-contrib/provider-sql/apis/cluster/singlestore/v1alpha1"
	"github.com/crossplane-contrib/provider-sql/pkg/clients/mysql"
)

// scopeKey identifies a GRANT scope (database.table), using quoted identifiers
// exactly as they appear in SHOW GRANTS output so observed and desired match.
type scopeKey struct {
	db    string
	table string
}

// privilegeMap maps a scope to its set of privileges (USAGE excluded).
type privilegeMap map[scopeKey]map[string]struct{}

// roleGrantRegex parses a "SHOW GRANTS FOR ROLE" line, e.g.
//
//	GRANT SELECT, INSERT ON `db`.* TO ROLE 'app_ro'
//
// Verified against a live SingleStore v8 instance.
var roleGrantRegex = regexp.MustCompile(`^GRANT (.+) ON (\S+)\.(\S+) TO ROLE \S+.*$`)

func defaultIdentifier(identifier *string) string {
	if identifier != nil && *identifier != "*" {
		return mysql.QuoteIdentifier(*identifier)
	}
	return "*"
}

func desiredPrivileges(blocks []v1alpha1.RolePrivilege) privilegeMap {
	out := privilegeMap{}
	for i := range blocks {
		b := blocks[i]
		scope := scopeKey{db: defaultIdentifier(b.Database), table: defaultIdentifier(b.Table)}
		if out[scope] == nil {
			out[scope] = map[string]struct{}{}
		}
		for _, p := range b.Privileges.ToStringSlice() {
			out[scope][normalize(p)] = struct{}{}
		}
	}
	return out
}

// normalize maps "ALL PRIVILEGES" to "ALL" so the alias compares equal, matching
// how SingleStore reports it.
func normalize(p string) string {
	return strings.ReplaceAll(strings.TrimSpace(p), "ALL PRIVILEGES", "ALL")
}

func parseRoleGrant(line string) (scopeKey, []string, bool) {
	m := roleGrantRegex.FindStringSubmatch(line)
	if len(m) != 4 {
		return scopeKey{}, nil, false
	}
	scope := scopeKey{db: m[2], table: m[3]}
	parts := strings.Split(m[1], ",")
	privs := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := normalize(p); t != "" {
			privs = append(privs, t)
		}
	}
	return scope, privs, true
}

func privilegesEqual(a, b privilegeMap) bool {
	for scope := range allScopes(a, b) {
		toGrant, toRevoke := diffSet(a[scope], b[scope])
		if len(toGrant) > 0 || len(toRevoke) > 0 {
			return false
		}
	}
	return true
}

// diffSet returns the privileges to grant (in desired, not observed) and to
// revoke (in observed, not desired).
func diffSet(desired, observed map[string]struct{}) (toGrant, toRevoke []string) {
	for p := range desired {
		if _, ok := observed[p]; !ok {
			toGrant = append(toGrant, p)
		}
	}
	for p := range observed {
		if _, ok := desired[p]; !ok {
			toRevoke = append(toRevoke, p)
		}
	}
	return toGrant, toRevoke
}

func allScopes(a, b privilegeMap) map[scopeKey]struct{} {
	out := make(map[scopeKey]struct{}, len(a)+len(b))
	for s := range a {
		out[s] = struct{}{}
	}
	for s := range b {
		out[s] = struct{}{}
	}
	return out
}
