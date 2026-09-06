package framework

import (
	"encoding/json"
	"errors"
)

// hookWire 保留插件字段名称，但以字符串或 null 传输错误，避免 error 接口丢失内容。
type hookAlias HookDTO

type hookWire struct {
	*hookAlias
	ErrMap map[string]*string
}

func (dto HookDTO) MarshalJSON() ([]byte, error) {
	wire := hookWire{hookAlias: (*hookAlias)(&dto)}
	if dto.ErrMap != nil {
		wire.ErrMap = make(map[string]*string, len(dto.ErrMap))
		for domain, err := range dto.ErrMap {
			if err == nil {
				wire.ErrMap[domain] = nil
			} else {
				text := err.Error()
				wire.ErrMap[domain] = &text
			}
		}
	}
	return json.Marshal(wire)
}

func (dto *HookDTO) UnmarshalJSON(data []byte) error {
	var decoded HookDTO
	wire := hookWire{hookAlias: (*hookAlias)(&decoded)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.ErrMap != nil {
		decoded.ErrMap = make(map[string]error, len(wire.ErrMap))
		for domain, text := range wire.ErrMap {
			if text == nil {
				decoded.ErrMap[domain] = nil
			} else {
				decoded.ErrMap[domain] = errors.New(*text)
			}
		}
	}
	*dto = decoded
	return nil
}
