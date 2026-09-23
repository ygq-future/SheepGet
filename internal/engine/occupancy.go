package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sheep-get/internal/task"
)

// 目标落点的占用判定。
//
// 「这个名字被占了」原先散在三个地方各自回答，口径还各不相同：只看磁盘、磁盘＋任务库、
// 磁盘＋任务库＋窗口队列项。而真正发名字的是文件信息窗口的队列，它对自己已经发给排队项的
// 名字是盲的——两个排队项会建议同一个名字，提交时撞到同一个落点。
//
// 这里把答案收成一份：占用来源只有三类，问题只有两个（这个名字被占了没有、下一个可用的
// 序号副本是哪个）。入口、重复链接裁决与文件信息窗口读的都是这里的结果。
//
// 三类来源：
//   - 磁盘：目标文件本身，以及下载中的 `<name>.sheepget` 中转文件；
//   - 任务库：同目录同名、且正在进行（排队中/下载中/处理中）的任务；
//   - 排队项：文件信息窗口已经发给其它排队项的名字（由调用方提供，并排除自己）。
//
// 已完成、失败、取消的历史记录不算占用：成品文件还在磁盘上的由磁盘来源覆盖，文件已经不在的
// 就是一条残留记录，新下载可以继续用它原来的名字。这不是遗漏——按状态放宽到「任何记录都算
// 占用」会让删掉成品后的续传、重下与序号副本一直往后排，名字只会越滚越大。

// Reserved 报告除调用方自己之外，是否已有别的排队项占着这个目标名字。
type Reserved func(dir, name string) bool

// Occupancy 是一份占用判定：任务库快照加上调用方提供的排队项来源。
type Occupancy struct {
	// Tasks 是任务库在组装这一刻的快照（存储层给出的副本）。判定要逐个候选名字反复问同一个
	// 问题，逐次读库会把同一个文件读很多遍，因此这里只留一份快照。
	Tasks []*task.Task
	// Reserved 为空表示调用方没有排队项来源（例如引擎内部没有窗口队列）。
	Reserved Reserved
}

// Taken 报告 dir 下的 name 是否已被占用。
func (o Occupancy) Taken(dir, name string) bool {
	if dir == "" || name == "" {
		return false
	}
	if FileExists(filepath.Join(dir, name)) {
		return true
	}
	for _, t := range o.Tasks {
		if !SamePath(t.Directory, dir) || !SameFilename(t.Filename, name) {
			continue
		}
		switch t.Status {
		case task.StatusDownloading, task.StatusQueued, task.StatusProcessing:
			return true
		}
	}
	return o.Reserved != nil && o.Reserved(dir, name)
}

// Suggest 返回这个落点可用的文件名：原名没被占用时就是原名，否则是第一个可用的序号副本。
// 冲突提示与序号副本因此出自同一个答案，不会出现「提示没冲突、建议却换了个名字」。
func (o Occupancy) Suggest(dir, name string) string {
	if !o.Taken(dir, name) {
		return name
	}
	return o.NumberedCopy(dir, name)
}

// NumberedCopy 返回第一个可用的序号副本名（`name (n).ext`），不看原名自己是否可用。
// 「序号副本」这个动作要的就是一份不同名的成品，因此它总是取编号名，而不是像建议那样在
// 原名可用时沿用原名。
func (o Occupancy) NumberedCopy(dir, name string) string {
	return NextNumberedCopy(name, func(candidate string) bool {
		return o.Taken(dir, candidate)
	})
}

// FileExists reports whether path or path.sheepget exists on disk.
func FileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	if _, err := os.Stat(path + ".sheepget"); err == nil {
		return true
	}
	return false
}

// DestinationOccupied reports whether this download's finished file already sits in dir.
// 既看用户当前选定的文件名，也看该链接历史任务的文件名：策略为「序号副本」时后端会先把
// 文件名改成 name (n).ext，此时真正占着位置、也真正需要用户决定怎么处理的是原名文件。
//
// 它回答的是重复链接裁决的「目标位置是否已有成品」，只认磁盘事实，与 Taken 的三类占用来源
// 不是同一个问题：排队项还没落盘，不算成品。
func DestinationOccupied(dir, filename string, dupTask *task.Task) bool {
	if dir == "" || filename == "" {
		return false
	}
	if FileExists(filepath.Join(dir, filename)) {
		return true
	}
	return dupTask != nil && dupTask.Filename != "" && dupTask.Filename != filename &&
		FileExists(filepath.Join(dir, dupTask.Filename))
}

var numberedSuffixRegex = regexp.MustCompile(`^(.*) \(\d+\)$`)

// NextNumberedCopy returns the first "name (n).ext" variant free according to taken.
func NextNumberedCopy(filename string, taken func(string) bool) string {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	if m := numberedSuffixRegex.FindStringSubmatch(stem); len(m) == 2 {
		stem = m[1]
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !taken(candidate) {
			return candidate
		}
	}
}
