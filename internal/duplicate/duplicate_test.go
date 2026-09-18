package duplicate

import (
	"testing"

	"sheep-get/internal/config"
)

func TestDecide_NoHistoryNeedsNoArbitration(t *testing.T) {
	f := Facts{Policy: config.DuplicatePolicyPrompt, DestinationOccupied: true}
	got := Decide(f)
	if got.Case != CaseNone {
		t.Errorf("case = %q, want %q（同名文件冲突独立处理，不进入重复链接裁决）", got.Case, CaseNone)
	}
	if got.Default != "" || len(got.Options) != 0 || got.ShowCompleted {
		t.Errorf("no history should decide nothing, got %+v", got)
	}
}

func TestDecide_DestinationOccupied(t *testing.T) {
	cases := []struct {
		name             string
		policy           config.DuplicateURLPolicy
		historyCompleted bool
		wantDefault      Action
	}{
		{"prompt 不给默认项，必须先选", config.DuplicatePolicyPrompt, true, ""},
		{"prompt 且历史未完成也不给默认项", config.DuplicatePolicyPrompt, false, ""},
		{"skip_show_completed 默认选中查看完成记录", config.DuplicatePolicySkipShowCompleted, true, ActionShowCompleted},
		{"continue_overwrite 且历史已完成 → 覆盖重下", config.DuplicatePolicyContinueOverwrite, true, ActionRedownload},
		{"continue_overwrite 且历史未完成 → 续传", config.DuplicatePolicyContinueOverwrite, false, ActionContinue},
		{"numbered_copy → 序号副本", config.DuplicatePolicyNumberedCopy, true, ActionCopy},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(Facts{
				Policy:              tc.policy,
				HasHistory:          true,
				HistoryCompleted:    tc.historyCompleted,
				DestinationOccupied: true,
			})
			if got.Case != CaseDestinationOccupied {
				t.Fatalf("case = %q, want %q", got.Case, CaseDestinationOccupied)
			}
			if len(got.Options) != 3 {
				t.Fatalf("options = %v, want 三个动作", got.Options)
			}
			if got.Default != tc.wantDefault {
				t.Errorf("default = %q, want %q", got.Default, tc.wantDefault)
			}
			// 默认项必须是用户看得到、点得到的选项之一，否则界面无法把它标为选中。
			if got.Default != "" {
				found := false
				for _, opt := range got.Options {
					if opt == got.Default {
						found = true
					}
				}
				if !found {
					t.Errorf("default %q 不在 options %v 中", got.Default, got.Options)
				}
			}
		})
	}
}

func TestDecide_HistoryCompletedWithFileAbsent(t *testing.T) {
	got := Decide(Facts{
		Policy:           config.DuplicatePolicyPrompt,
		HasHistory:       true,
		HistoryCompleted: true,
	})
	if got.Case != CaseHistoryFileAbsent {
		t.Fatalf("case = %q, want %q", got.Case, CaseHistoryFileAbsent)
	}
	// 没有可续传的进度也没有可覆盖的文件，只剩重新下载，因此不给选项。
	if got.Default != ActionRedownload {
		t.Errorf("default = %q, want %q", got.Default, ActionRedownload)
	}
	if len(got.Options) != 0 {
		t.Errorf("options = %v, want 空（不需要用户选择）", got.Options)
	}
}

func TestDecide_HistoryUnfinishedKeepsProgress(t *testing.T) {
	for _, policy := range []config.DuplicateURLPolicy{
		config.DuplicatePolicyPrompt,
		config.DuplicatePolicySkipShowCompleted,
		config.DuplicatePolicyContinueOverwrite,
		config.DuplicatePolicyNumberedCopy,
	} {
		t.Run(string(policy), func(t *testing.T) {
			got := Decide(Facts{Policy: policy, HasHistory: true})
			if got.Case != CaseHistoryUnfinished {
				t.Fatalf("case = %q, want %q", got.Case, CaseHistoryUnfinished)
			}
			// 历史任务未完成且目标位置没有成品文件：续传是唯一有意义的动作。
			// 任何策略都不得把这种局面变成丢弃已有分片的重下或另存副本。
			if got.Default != ActionContinue {
				t.Errorf("default = %q, want %q", got.Default, ActionContinue)
			}
			if len(got.Options) != 0 {
				t.Errorf("options = %v, want 空（无成品文件时重下与副本无意义）", got.Options)
			}
		})
	}
}

func TestDecide_ShowCompletedOnlyWhenPolicyAsksAndHistoryCompleted(t *testing.T) {
	cases := []struct {
		name      string
		facts     Facts
		wantShown bool
	}{
		{
			name:      "skip 策略且历史已完成",
			facts:     Facts{Policy: config.DuplicatePolicySkipShowCompleted, HasHistory: true, HistoryCompleted: true},
			wantShown: true,
		},
		{
			name:      "skip 策略但历史未完成",
			facts:     Facts{Policy: config.DuplicatePolicySkipShowCompleted, HasHistory: true},
			wantShown: false,
		},
		{
			name:      "skip 策略但目标位置已有成品文件",
			facts:     Facts{Policy: config.DuplicatePolicySkipShowCompleted, HasHistory: true, HistoryCompleted: true, DestinationOccupied: true},
			wantShown: true,
		},
		{
			name:      "其它策略不唤起",
			facts:     Facts{Policy: config.DuplicatePolicyPrompt, HasHistory: true, HistoryCompleted: true},
			wantShown: false,
		},
		{
			name:      "没有历史任务",
			facts:     Facts{Policy: config.DuplicatePolicySkipShowCompleted},
			wantShown: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.facts).ShowCompleted; got != tc.wantShown {
				t.Errorf("showCompleted = %v, want %v", got, tc.wantShown)
			}
		})
	}
}
