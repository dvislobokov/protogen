// White-box tests for rule matching. This file must not import gen (which
// imports authz), hence the separate in-package test file.
package authz

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		name string
		rule *Rule
		have []string
		want bool
	}{
		{"nil rule passes", nil, nil, true},
		{"empty rule passes", &Rule{}, nil, true},
		{"any_of hit", &Rule{AnyOf: []string{"a", "b"}}, []string{"b"}, true},
		{"any_of miss", &Rule{AnyOf: []string{"a", "b"}}, []string{"c"}, false},
		{"all_of complete", &Rule{AllOf: []string{"a", "b"}}, []string{"b", "a", "c"}, true},
		{"all_of partial", &Rule{AllOf: []string{"a", "b"}}, []string{"a"}, false},
		{"none_of clean", &Rule{NoneOf: []string{"banned"}}, []string{"user"}, true},
		{"none_of violated", &Rule{NoneOf: []string{"banned"}}, []string{"user", "banned"}, false},
		{"combined pass", &Rule{AnyOf: []string{"a", "b"}, AllOf: []string{"c"}, NoneOf: []string{"x"}}, []string{"a", "c"}, true},
		{"combined none_of fails", &Rule{AnyOf: []string{"a"}, NoneOf: []string{"x"}}, []string{"a", "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := match(tt.rule, tt.have); got != tt.want {
				t.Fatalf("match(%v, %v) = %v, want %v", tt.rule, tt.have, got, tt.want)
			}
		})
	}
}

func TestLookupRequirementsUnknownMethod(t *testing.T) {
	if req := lookupRequirements("/no.such.Service/Nope"); req != nil {
		t.Fatalf("unknown service must yield nil requirements, got %v", req)
	}
	if req := lookupRequirements("garbage"); req != nil {
		t.Fatalf("malformed method must yield nil requirements, got %v", req)
	}
}
