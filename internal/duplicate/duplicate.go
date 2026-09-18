// Package duplicate 裁决重复链接：给定全局策略、历史任务状态与目标位置的客观事实，
// 得出界面应当呈现哪些动作、以及默认执行哪个动作。
//
// 它是这条规则的唯一实现。文件信息窗口只渲染裁决结果并原样回传用户选中的动作，
// 不推导策略含义、也不自行判断该不该给选项（后端为唯一事实来源，ADR-0002）。
package duplicate

import "sheep-get/internal/config"

// Action 是一次裁决给出的可执行动作。取值与 engine.ResolveDuplicate 的执行分支一一对应，
// 因此后端收到界面回传的动作后可以直接执行，不需要再次翻译。
type Action string

const (
	// ActionContinue 续传历史任务，保留已下载的分片。
	ActionContinue Action = "continue"
	// ActionRedownload 丢弃历史任务与残留分片，重新创建任务下载。
	ActionRedownload Action = "redownload"
	// ActionCopy 以序号副本另存，历史任务保持不动。
	ActionCopy Action = "copy"
	// ActionShowCompleted 不下载，只查看历史任务的完成记录。
	ActionShowCompleted Action = "show_completed"
	// ActionReuse 复用其他目录里已有的成品文件，不重新下载。
	//
	// 它不由重复链接策略推导，而是来自「其他目录已存在同一份成品」这一独立事实，
	// 由界面在存在可复用文件时提供额外入口；这里保留取值以便提交契约完整。
	ActionReuse Action = "reuse"
)

// Case 是目标位置与历史任务共同构成的客观局面。界面按它决定显示哪种提示。
type Case string

const (
	// CaseNone 没有同一 URL 的历史任务，无需裁决；同名文件冲突另行独立处理。
	CaseNone Case = ""
	// CaseDestinationOccupied 目标位置已有成品文件：三个动作都有意义，用户需要选择。
	CaseDestinationOccupied Case = "destination_occupied"
	// CaseHistoryFileAbsent 历史任务已完成，但成品文件已不在目标位置（被删除或移走）：
	// 没有可续传的进度，也没有可覆盖的文件，只剩重新下载。
	CaseHistoryFileAbsent Case = "history_file_absent"
	// CaseHistoryUnfinished 历史任务尚未完成：分片就是全部价值所在，续传是唯一有意义的动作。
	CaseHistoryUnfinished Case = "history_unfinished"
)

// Facts 是裁决的全部输入事实，全部来自后端观察，不含界面推断。
type Facts struct {
	// Policy 是配置中心里的重复链接全局策略。
	Policy config.DuplicateURLPolicy
	// HasHistory 表示存在同一 URL 的历史任务。
	HasHistory bool
	// HistoryCompleted 表示该历史任务已完成（成品文件曾成功落盘）。
	HistoryCompleted bool
	// DestinationOccupied 表示目标保存位置已存在同名成品文件。
	DestinationOccupied bool
}

// Decision 是裁决结果。
type Decision struct {
	Case Case `json:"case"`
	// Options 是界面应当呈现的选项，顺序即展示顺序。为空表示动作已定、无需用户选择。
	Options []Action `json:"options,omitempty"`
	// Default 是用户没有另行选择时执行的动作，同时也是界面默认选中项。
	// 为空表示必须先由用户选定一项，否则不能开始下载。
	Default Action `json:"default,omitempty"`
	// ShowCompleted 表示登记这次重复时应当唤起共享下载进度窗口的完成区域。
	// 由「跳过并显示完成」策略加上「历史任务已完成」这个事实共同决定。
	ShowCompleted bool `json:"showCompleted,omitempty"`
}

// Decide 按事实给出唯一裁决。
func Decide(f Facts) Decision {
	if !f.HasHistory {
		return Decision{Case: CaseNone}
	}

	showCompleted := f.Policy == config.DuplicatePolicySkipShowCompleted && f.HistoryCompleted

	if !f.DestinationOccupied {
		// 目标位置没有成品文件：无文件可覆盖、无副本可区分，因此都不给选项，动作直接定下。
		if f.HistoryCompleted {
			return Decision{
				Case:          CaseHistoryFileAbsent,
				Default:       ActionRedownload,
				ShowCompleted: showCompleted,
			}
		}
		return Decision{
			Case:          CaseHistoryUnfinished,
			Default:       ActionContinue,
			ShowCompleted: showCompleted,
		}
	}

	// 目标位置已有成品文件：三个动作都有意义，交给用户选择，按策略决定默认选中项。
	return Decision{
		Case:          CaseDestinationOccupied,
		Options:       []Action{ActionShowCompleted, overwriteAction(f.HistoryCompleted), ActionCopy},
		Default:       defaultForOccupied(f.Policy, f.HistoryCompleted),
		ShowCompleted: showCompleted,
	}
}

// overwriteAction 是「继续覆盖」这一项实际执行的动作：历史任务还活着就接着传，
// 已经完成（成品就在磁盘上）就丢掉旧记录与旧文件重新下载。
func overwriteAction(historyCompleted bool) Action {
	if historyCompleted {
		return ActionRedownload
	}
	return ActionContinue
}

// defaultForOccupied 给出目标位置已有成品文件时的默认选中项。
//
// 「跳过并显示完成」默认选中查看完成记录：这正是该策略的字面含义——不下载，只把完成记录
// 拿到眼前；文件信息窗口仍然给出其余选项供用户进一步操作。
// 「询问」不给默认项，它本来就是要用户选。
func defaultForOccupied(policy config.DuplicateURLPolicy, historyCompleted bool) Action {
	switch policy {
	case config.DuplicatePolicySkipShowCompleted:
		return ActionShowCompleted
	case config.DuplicatePolicyContinueOverwrite:
		return overwriteAction(historyCompleted)
	case config.DuplicatePolicyNumberedCopy:
		return ActionCopy
	default:
		return ""
	}
}
