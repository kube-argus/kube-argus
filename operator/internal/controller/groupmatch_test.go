/*
Copyright 2026.

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

package controller

import "testing"

// ponytail: one runnable check on the only non-trivial decision (annotation -> membership).
func TestGroupMatches(t *testing.T) {
	groups := map[string]bool{"group/g123": true}
	cases := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{"match", map[string]string{groupAnnotation: "group/g123"}, true},
		{"not in set", map[string]string{groupAnnotation: "group/other"}, false},
		{"no annotation", map[string]string{"unrelated": "x"}, false},
		{"nil annotations", nil, false},
		{"empty value", map[string]string{groupAnnotation: ""}, false},
	}
	for _, c := range cases {
		if got := groupMatches(groups, c.annotations); got != c.want {
			t.Errorf("%s: groupMatches = %v, want %v", c.name, got, c.want)
		}
	}
}
