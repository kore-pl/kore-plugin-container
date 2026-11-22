package containerop

import (
	"encoding/json"

	"github.com/pengtang1024/kore/pkg/pluginapi"
)

// callInspect 处理 container.inspect：校验 id 形状后返回结构化占位，不真连 daemon。
func callInspect(raw json.RawMessage, exec Executor) (json.RawMessage, *pluginapi.Error) {
	var p inspectParams
	if perr := decodeParams(raw, &p); perr != nil {
		return nil, perr
	}
	idKind, perr := ValidateID(p.ID)
	if perr != nil {
		return nil, perr
	}
	container, err := exec.Inspect(p.ID)
	if err != nil {
		return nil, classify(err)
	}
	if container == nil {
		container = &Container{}
	}
	return encode(InspectResult{
		ID:      p.ID,
		IDKind:  idKind,
		Name:    firstOr(container.Names, p.ID),
		Image:   container.Image,
		Status:  container.Status,
		Applied: false, // 骨架不真应用任何运行时操作
	})
}

// firstOr 返回切片首元素，空切片时返回 fallback。
func firstOr(values []string, fallback string) string {
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return fallback
}
