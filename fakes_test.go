package main

import "github.com/pengtang1024/kore-plugin-container/internal/containerop"

// fakeExecutor 是 main 包测试用的执行面：只回固定数据、记录调用顺序，不触达 docker。
type fakeExecutor struct {
	entries []containerop.ContainerEntry
	inspect *containerop.InspectResult
	logs    containerop.LogsResult
	stats   containerop.StatsResult
	images  []containerop.ImageEntry
	ref     containerop.ContainerRef
	err     error
	calls   []string
}

// newFakeExecutor 构造带一条 web 容器数据的执行面。
func newFakeExecutor() *fakeExecutor {
	const fullID = "0123456789ab0123456789ab0123456789ab0123456789ab0123456789ab"
	return &fakeExecutor{
		entries: []containerop.ContainerEntry{{
			ID: "0123456789ab", Name: "web", Image: "nginx:alpine", State: "running",
			Status: "Up 1 minute", Ports: "0.0.0.0:18081->80/tcp",
		}},
		inspect: &containerop.InspectResult{
			ID: fullID, Name: "web", Image: "nginx:alpine",
			State: containerop.InspectState{Status: "running", Running: true},
		},
		logs:  containerop.LogsResult{ID: "web", Lines: []string{"started"}},
		stats: containerop.StatsResult{ID: "web", Name: "web", PIDs: 1},
		ref:   containerop.ContainerRef{ID: fullID, Name: "web"},
	}
}

func (f *fakeExecutor) List(all bool) ([]containerop.ContainerEntry, error) {
	f.calls = append(f.calls, "list")
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func (f *fakeExecutor) Inspect(id string) (*containerop.InspectResult, error) {
	f.calls = append(f.calls, "inspect")
	if f.err != nil {
		return nil, f.err
	}
	return f.inspect, nil
}

func (f *fakeExecutor) Logs(id string, tail int, since string) (containerop.LogsResult, error) {
	f.calls = append(f.calls, "logs")
	if f.err != nil {
		return containerop.LogsResult{}, f.err
	}
	return f.logs, nil
}

func (f *fakeExecutor) Start(id string) error {
	f.calls = append(f.calls, "start")
	return f.err
}

func (f *fakeExecutor) Stop(id string, timeout int) error {
	f.calls = append(f.calls, "stop")
	return f.err
}

func (f *fakeExecutor) Restart(id string, timeout int) error {
	f.calls = append(f.calls, "restart")
	return f.err
}

func (f *fakeExecutor) Stats(id string) (containerop.StatsResult, error) {
	f.calls = append(f.calls, "stats")
	if f.err != nil {
		return containerop.StatsResult{}, f.err
	}
	return f.stats, nil
}

func (f *fakeExecutor) Images() ([]containerop.ImageEntry, error) {
	f.calls = append(f.calls, "images")
	if f.err != nil {
		return nil, f.err
	}
	return f.images, nil
}

func (f *fakeExecutor) Pull(ref string) (containerop.PullResult, error) {
	f.calls = append(f.calls, "pull")
	if f.err != nil {
		return containerop.PullResult{}, f.err
	}
	return containerop.PullResult{Ref: ref, OK: true, Message: "Status: Downloaded newer image for " + ref}, nil
}

func (f *fakeExecutor) RemoveImage(ref string, force bool) error {
	f.calls = append(f.calls, "rmi")
	return f.err
}

func (f *fakeExecutor) Update(id string, update containerop.ContainerUpdate) error {
	f.calls = append(f.calls, "update")
	return f.err
}

func (f *fakeExecutor) Exec(req containerop.ExecRequest) (containerop.ExecResult, error) {
	return containerop.ExecResult{ExitCode: 0}, nil
}

func (f *fakeExecutor) Resolve(id string) (containerop.ContainerRef, error) {
	f.calls = append(f.calls, "resolve")
	if f.err != nil {
		return containerop.ContainerRef{}, f.err
	}
	if f.ref.ID == "" {
		return containerop.ContainerRef{ID: id, Name: id}, nil
	}
	return f.ref, nil
}

// testRuntime 构造注入假执行面的 Runtime（GOOS 固定 linux，避免测试依赖宿主平台）。
func testRuntime(exec containerop.Executor) *containerop.Runtime {
	return containerop.NewRuntime(containerop.Options{GOOS: "linux"}, exec)
}
