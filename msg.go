package hubx

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
