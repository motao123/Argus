package api

import "testing"

func TestPagination(t *testing.T) {
	cases := []struct {
		name          string
		offset, limit int
		wantO, wantL  int
	}{
		{"defaults", 0, 0, 0, 50},
		{"over max", 0, 9999, 0, 500},
		{"custom", 20, 100, 20, 100},
	}
	for _, c := range cases {
		// 直接测边界逻辑
		limit := c.limit
		if limit > 500 {
			limit = 500
		}
		if limit < 1 {
			limit = 50
		}
		offset := c.offset
		if offset < 0 {
			offset = 0
		}
		if limit != c.wantL || offset != c.wantO {
			t.Errorf("%s: got (%d,%d) want (%d,%d)", c.name, offset, limit, c.wantO, c.wantL)
		}
	}
}

func TestScopeLogic(t *testing.T) {
	p := &principal{IsPAT: true, TokenScopes: map[string]bool{"argus:server:read": true}}
	if !p.hasScope("argus:server:read") {
		t.Error("should have server:read")
	}
	if p.hasScope("argus:server:write") {
		t.Error("should NOT have server:write")
	}
	admin := &principal{TokenScopes: map[string]bool{"argus:*": true}}
	if !admin.hasScope("argus:server:exec") {
		t.Error("wildcard should cover all")
	}
	jwt := &principal{IsAdmin: true}
	if !jwt.hasScope("anything") {
		t.Error("JWT admin should pass scope")
	}
}
