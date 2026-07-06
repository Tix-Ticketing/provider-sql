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

// Package internal holds set helpers shared by the SingleStore reconcilers,
// which repeatedly diff a desired set of names/privileges against the observed
// set to compute what to add and remove.
package internal

import "sort"

// StringSet builds a set from a slice.
func StringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

// SortedKeys returns the set's members in sorted order.
func SortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StringSetDiff returns the items to add (in desired, not observed) and to
// remove (in observed, not desired) to reconcile observed toward desired.
// Both results are sorted.
func StringSetDiff(desired, observed map[string]struct{}) (toAdd, toRemove []string) {
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
