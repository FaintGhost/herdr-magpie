package magpie

import (
	"slices"
	"testing"
)

func TestQuotasFixture(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"magpie quota --json": string(readFixture(t, "quota_edge.json"))}}
	quotas, err := Quotas(t.Context(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"magpie quota --json"}) {
		t.Errorf("calls = %q", r.calls)
	}
	checkGolden(t, "quota_edge", quotas)
}

func TestAccountsFixture(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"magpie accounts --json": string(readFixture(t, "accounts_edge.json"))}}
	accounts, err := Accounts(t.Context(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"magpie accounts --json"}) {
		t.Errorf("calls = %q", r.calls)
	}
	checkGolden(t, "accounts_edge", accounts)
}

func TestQuotaAndAccountDecodeErrors(t *testing.T) {
	for _, in := range []string{"", "{", `{"not":"a list"}`} {
		r := &fakeRunner{out: map[string]string{
			"magpie quota --json":    in,
			"magpie accounts --json": in,
		}}
		if _, err := Quotas(t.Context(), r, ""); err == nil {
			t.Errorf("Quotas(%q) succeeded, want a decode error", in)
		}
		if _, err := Accounts(t.Context(), r, ""); err == nil {
			t.Errorf("Accounts(%q) succeeded, want a decode error", in)
		}
	}
}
