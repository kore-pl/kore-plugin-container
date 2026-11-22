package containerop

import (
	"encoding/json"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callList 处理 container.list：参数必须是对象，返回空容器列表的结构化占位。
func callList(raw json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p listParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	containers, err := exec.List()
	if err != nil {
		return nil, classify(err)
	}
	if containers == nil {
		containers = []Container{}
	}
	// managed=false 明确告知调用方：骨架未连接任何真实运行时。
	return encode(ListResult{Containers: containers, Count: len(containers), Managed: false})
}

// decodeParams 校验 params 是 JSON 对象并反序列化到目标形状。
func decodeParams(raw json.RawMessage, target any) *pluginapi.Error {
	if len(raw) == 0 || string(raw) == "null" {
		return invalid("缺少必填参数对象")
	}
	if raw[0] != '{' {
		return invalid("params 必须是 JSON 对象")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return invalid("params 解析失败: " + err.Error())
	}
	return nil
}

// encode 把结果编码为 json.RawMessage；序列化失败归 E_INTERNAL。
func encode(v any) (json.RawMessage, *pluginapi.Error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, internalErr("序列化结果失败: " + err.Error())
	}
	return data, nil
}

// noopExecutor 是骨架默认执行面：不连 daemon，只读返回空列表/空条目。
type noopExecutor struct{}

func (noopExecutor) List() ([]Container, error) { return nil, nil }
func (noopExecutor) Inspect(id string) (*Container, error) {
	return &Container{}, nil
}
