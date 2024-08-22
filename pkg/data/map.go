package data

import (
	"google.golang.org/protobuf/types/known/structpb"
)

type Map struct {
	Fields map[string]Value
}

func NewMap(m map[string]Value) (mp *Map) {
	if m == nil {
		m = map[string]Value{}
	}
	return &Map{
		Fields: m,
	}
}

func (*Map) isValue() {}

func (m *Map) ToStructValue() (v *structpb.Value, err error) {
	mp := &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	for k, v := range m.Fields {
		if v == nil {
			mp.Fields[k] = structpb.NewNullValue()
		} else {
			mp.Fields[k], err = v.ToStructValue()
			if err != nil {
				return nil, err
			}
		}

	}
	return structpb.NewStructValue(mp), nil
}
