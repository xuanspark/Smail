package dto

import "fmt"

type SearchTag struct {
	Type    int8 `json:"type"`     // -1 不限
	Status  int8 `json:"status"`   // -1 不限
	GroupId int  `json:"group_id"` // -1 不限
}

func (t SearchTag) ToString() string {
	return fmt.Sprintf(`{"type":%d,"status":%d,"group_id":%d}`, t.Type, t.Status, t.GroupId)
}
