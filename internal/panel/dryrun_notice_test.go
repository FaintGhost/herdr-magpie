package panel

import "testing"

func TestDryRunMarksSuccessNotices(t *testing.T) {
	m := &Model{dryRun: true}
	m.setNotice(noticeOK, "switched Codex to someone")
	if m.notice != "[dry-run] switched Codex to someone" {
		t.Errorf("ok notice = %q", m.notice)
	}
	m.setNotice(noticeBad, "failed")
	if m.notice != "failed" {
		t.Errorf("bad notice = %q, want it unmarked", m.notice)
	}
	m = &Model{}
	m.setNotice(noticeOK, "applied profile x")
	if m.notice != "applied profile x" {
		t.Errorf("notice without dry run = %q", m.notice)
	}
}
