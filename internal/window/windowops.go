package window

// 队列的窗口副作用统一走这里，原因只有一个：窗口调用可能长时间不返回。
//
// Wails 的 Show / Focus / Hide / Emit 都会把调用同步派发回主线程并等它执行完
// （application.InvokeSync 没有超时）。主线程同时还要处理窗口消息、文件对话框和 WebView
// 回调，被占住是常态而不是异常。窗口副作用一旦在队列锁内同步执行，两件事会同时发生：
// 交接请求的响应被拖过扩展 2.5 秒的等待上限（扩展按「没收到响应」把下载还给浏览器，而桌面端
// 其实已经受理，用户拿到两份下载），以及所有队列操作（读当前项、取消、下一次交接）一起等在
// 那个不返回的调用上。
//
// 因此收集与执行分开：windowActions 在锁内收集（次序与队列状态一致），windowOps 在锁外执行。

// windowOps 执行一批窗口副作用。真实实现见 newAsyncOps：提交方只提交，不等待。
type windowOps func(task func())

// windowOpsBuffer 用来吸收「执行方正卡在主线程上」这段时间里提交的副作用。它满意味着主线程
// 已经长时间不响应（应用此时本身也不可用了），提交会转为等待——这比丢掉「显示窗口」这类
// 副作用更可接受。
const windowOpsBuffer = 32

// newAsyncOps 把窗口副作用交给一条独立的串行队列。
//
// 串行是有意的，不能改成「一次一个 goroutine」：界面上的当前项必须与队列里的 activeIndex
// 对应，先显示、后投递的次序错了，用户可能在错的那一项上点确认。
func newAsyncOps() windowOps {
	tasks := make(chan func(), windowOpsBuffer)
	go func() {
		for task := range tasks {
			task()
		}
	}()
	return func(task func()) { tasks <- task }
}

// windowActions 收集一次队列操作产生的窗口副作用，并把它们作为一个整体提交执行。
type windowActions struct {
	view  WindowView
	tasks []func()
}

func newWindowActions(view WindowView) *windowActions {
	return &windowActions{view: view}
}

func (a *windowActions) show() {
	if a.view != nil {
		a.tasks = append(a.tasks, a.view.Show)
	}
}

func (a *windowActions) hide() {
	if a.view != nil {
		a.tasks = append(a.tasks, a.view.Hide)
	}
}

func (a *windowActions) focus() {
	if a.view != nil {
		a.tasks = append(a.tasks, a.view.Focus)
	}
}

func (a *windowActions) emit(event string, data any) {
	if a.view == nil {
		return
	}
	a.tasks = append(a.tasks, func() { a.view.Emit(event, data) })
}

// add 收集一个同样会阻塞调用方的副作用，例如唤起下载进度窗口。
func (a *windowActions) add(task func()) {
	a.tasks = append(a.tasks, task)
}

// run 把收集到的副作用作为一个整体交给执行器。整体提交是为了让它们之间的相对次序
// （先显示、再置前、最后投递）不被其他操作的副作用插进来。
func (a *windowActions) run(run windowOps) {
	if len(a.tasks) == 0 {
		return
	}
	tasks := a.tasks
	run(func() {
		for _, task := range tasks {
			task()
		}
	})
}
