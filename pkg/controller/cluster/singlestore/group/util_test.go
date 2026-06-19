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
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBareUsername(t *testing.T) {
	// SHOW USERS FOR GROUP returns the first column as 'user'@'host'; GRANT GROUP
	// ... TO expects the bare username.
	cases := map[string]string{
		"'svc_re'@'%'":        "svc_re",
		"'svc_re'@'10.0.0.1'": "svc_re",
		"'admin'@'localhost'": "admin",
		"svc_re":              "svc_re",
		"'a@b.com'@'%'":       "a@b.com",
	}
	for in, want := range cases {
		if got := bareUsername(in); got != want {
			t.Errorf("bareUsername(%q): want %q got %q", in, want, got)
		}
	}
}

func TestDiff(t *testing.T) {
	desired := set([]string{"a", "b", "c"})
	observed := set([]string{"b", "c", "d"})
	toAdd, toRemove := diff(desired, observed)
	if diff := cmp.Diff([]string{"a"}, toAdd); diff != "" {
		t.Errorf("toAdd -want +got:\n%s", diff)
	}
	if diff := cmp.Diff([]string{"d"}, toRemove); diff != "" {
		t.Errorf("toRemove -want +got:\n%s", diff)
	}
}
