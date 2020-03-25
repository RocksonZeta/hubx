package hubx

import "encoding/json"

type Message struct {
	Subject string
	Data    interface{}
}

func (m Message) Unmarshal(result interface{}) error {
	return DefaultUnmarhaller(m.Data.([]byte), result)
}
func (m Message) Marshal() ([]byte, error) {
	return DefaultMarhaller(m)
}

type PartialMessage struct {
	Subject string
	Data    json.RawMessage
}

func (p *PartialMessage) UnmarshalData(result interface{}) error {
	return DefaultUnmarhaller(p.Data, result)
}
func (p *PartialMessage) Marshal() ([]byte, error) {
	return DefaultMarhaller(p)
}
func (p *PartialMessage) Unmarshal(bs []byte) error {
	return DefaultUnmarhaller(bs, p)
}
func (p *PartialMessage) String() string {
	bs, _ := p.Marshal()
	return string(bs)
}
