package hubx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"time"
)

var DefaultUnmarhaller = func(dataBs []byte, obj interface{}) error {
	return json.Unmarshal(dataBs, obj)
}

var DefaultMarhaller = func(obj interface{}) ([]byte, error) {
	return json.Marshal(obj)
}

func DefaultRawMessageUnmarhaller(bs []byte) (PartialMessage, error) {
	var m PartialMessage
	err := json.Unmarshal(bs, &m)
	return m, err
}

type Broadcaster interface {
	Send() chan<- []byte
	Close() chan<- bool
}

//WsFilter Websocket message filter
type WsFilter func(client *Client, msg PartialMessage, next func())

//WsListener Websocket message listener
type WsListener func(client *Client, msg PartialMessage)

//BcFilter broadcast message filter
type BcFilter func(msg PartialMessage, next func())

//BcListener broadcast message listener
type BcListener func(msg PartialMessage)

type Options struct {
	WriteTimeout   time.Duration
	PongTimeout    time.Duration
	PingPeriod     time.Duration
	TickerPeriod   time.Duration
	MaxMessageSize int64
	Logger         io.Writer
	LoggerLevel    int
	ChannelSize    int
}
type Hubx struct {
	options          Options
	clients          map[*Client]bool
	wsMessageChan    chan *RawMessage
	broadcastMessage chan []byte
	broadcaster      Broadcaster
	register         chan *Client
	unregister       chan *Client
	closeChan        chan bool
	wsFilters        []WsFilter            //filter for local message
	listeners        map[string]WsListener //listen local message
	bcFilters        []BcFilter            //filter for network broadcast message
	BcListeners      map[string]BcListener // subcribe redis message
	log              *Logger
	ticker           *time.Ticker

	BeforeJoin            func(client *Client) error
	AfterJoin             func(client *Client)
	BeforeLeave           func(client *Client)
	AfterLeave            func(client *Client)
	Unmarshaller          func(dataBs []byte, obj interface{}) error
	Marshaller            func(obj interface{}) ([]byte, error)
	RawMessageUnmarhaller func(bs []byte) (PartialMessage, error)
	Ticker                func()
}

func Go(fn func()) {
	go func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Println(err)
				debug.PrintStack()
			}
		}()
		fn()
	}()
}

func setOptionDefault(options *Options) {
	if options.ChannelSize <= 0 {
		options.ChannelSize = 1
	}
	if options.TickerPeriod <= 0 {
		options.TickerPeriod = 1 * time.Second
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = 5 * time.Second
	}
	if options.PongTimeout <= 0 {
		options.PongTimeout = 60 * time.Second
	}
	if options.PingPeriod <= 0 {
		options.PingPeriod = options.PongTimeout * 9 / 10
	}
	if options.MaxMessageSize <= 0 {
		options.MaxMessageSize = 4 * 1024 * 1024
	}
}

func New(options Options) (*Hubx, error) {
	setOptionDefault(&options)
	r := &Hubx{
		options:               options,
		wsMessageChan:         make(chan *RawMessage, options.ChannelSize),
		broadcastMessage:      make(chan []byte, options.ChannelSize),
		closeChan:             make(chan bool),
		register:              make(chan *Client, options.ChannelSize),
		unregister:            make(chan *Client, options.ChannelSize),
		clients:               make(map[*Client]bool),
		listeners:             make(map[string]WsListener),
		BcListeners:           make(map[string]BcListener),
		Marshaller:            DefaultMarhaller,
		RawMessageUnmarhaller: DefaultRawMessageUnmarhaller,
	}
	r.log = NewLogger(r.options.Logger, "github.com/RocksonZeta/hubx.Hubx", r.options.LoggerLevel)
	r.log.Trace("New")
	return r, nil
}

//Start hubx actor
func (h *Hubx) Start() {
	h.ticker = time.NewTicker(h.options.TickerPeriod)
	Go(h.run)
}

//SetBroadcast we should set broadcaster before start
func (h *Hubx) SetBroadcaster(broadcaster Broadcaster) {
	h.broadcaster = broadcaster
}

//BroadcastMessage recevie message from network broadcast like redis pubsub
func (h *Hubx) BroadcastMessage() chan<- []byte {
	return h.broadcastMessage
}

func (h *Hubx) run() {
	defer h.close()
	for {
		select {
		case <-h.closeChan:
			return
		case <-h.ticker.C:
			if h.Ticker != nil {
				h.Ticker()
			}
		case client := <-h.register:
			if h.BeforeJoin != nil {
				err := h.BeforeJoin(client)
				if err != nil {
					continue
				}
			}
			h.clients[client] = true
			if h.AfterJoin != nil {
				h.AfterJoin(client)
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				if h.BeforeLeave != nil {
					h.BeforeLeave(client)
				}
				delete(h.clients, client)
				close(client.send)
				if h.AfterLeave != nil {
					h.AfterLeave(client)
				}
			}
		case msg := <-h.wsMessageChan:
			m, err := h.RawMessageUnmarhaller(msg.Msg)
			if err != nil {
				h.log.Error("hubx receive bad message")
				close(msg.Client.send)
				delete(h.clients, msg.Client)
				return
			}
			h.callWithWsFilters(0, msg.Client, m)
		case msg := <-h.broadcastMessage:
			h.log.Trace("receive from broadcastMessage " + string(msg))
			m, err := h.RawMessageUnmarhaller(msg)
			if err != nil {
				h.log.Error("hubx receive bad message from redis channel")
			}
			h.callWithbcFilters(0, m)
		}
	}
}

//Close chan
func (h *Hubx) Close() chan<- bool {
	return h.closeChan
}
func (h *Hubx) CloseAsync() {
	h.log.Trace("Close")
	Go(func() {
		h.closeChan <- true
	})
}

//On set broadcast message listener
func (h *Hubx) On(subject string, cb BcListener) {
	h.BcListeners[subject] = cb
}

//Broadcast message to broadcaster
func (h *Hubx) Broadcast(subject string, data interface{}) {
	h.log.Trace("Send - subject:" + subject)
	bs, err := h.marshal(subject, data)
	if err != nil {
		h.log.Error("Send - marshal failed. subject:" + subject + ",error:" + err.Error())
		return
	}
	Go(func() {
		h.broadcaster.Send() <- bs
	})
}

//Userfilter for broadcaster's message
func (h *Hubx) Use(filter BcFilter) {
	h.bcFilters = append(h.bcFilters, filter)
}

//UseWs filter for websocket's message
func (h *Hubx) UseWs(filter WsFilter) {
	h.wsFilters = append(h.wsFilters, filter)
}

//OnWs set websocket message listener
func (h *Hubx) OnWs(subject string, cb WsListener) {
	h.listeners[subject] = cb
}

func (h *Hubx) BroadcastWs(subject string, msg interface{}) {
	h.log.Trace("BroadcastWs - subject:" + subject)
	go func() {
		bs, err := h.marshal(subject, msg)
		if err != nil {
			h.log.Error("hubx.Hubx.BroadcastWs - marshal failed. subject:" + subject + ",error:" + err.Error())
			return
		}
		timer := time.NewTimer(h.options.WriteTimeout)
		for client := range h.clients {
			timer.Reset(h.options.WriteTimeout)
			select {
			case client.send <- bs:
			case <-timer.C:
				close(client.send)
				delete(h.clients, client)
			}
		}
	}()
}

func (h *Hubx) SendWs(subject string, data interface{}, clients ...*Client) {
	h.log.Trace("SendWs - subject:" + subject)
	Go(func() {
		bs, err := h.marshal(subject, data)
		if err != nil {
			h.log.Error("SendWs - marshal failed. subject:" + subject + ",error:" + err.Error())
			return
		}
		timer := time.NewTimer(h.options.WriteTimeout)
		for client := range h.clients {
			timer.Reset(h.options.WriteTimeout)
			select {
			case client.send <- bs:
			case <-timer.C:
				close(client.send)
				delete(h.clients, client)
			}
		}
	})
}
func (h *Hubx) SendWsWithCtx(ctx context.Context, subject string, data interface{}, clients ...*Client) {
	h.log.Trace("SendWsWithCtx - subject:" + subject)
	Go(func() {

		m := Message{Subject: subject, Data: data}
		bs, err := h.Marshaller(m)
		if err != nil {
			h.log.Error("SendWsWithCtx - marshal failed. subject:" + subject + ",error:" + err.Error())
			return
		}
		for client := range h.clients {
			select {
			case client.send <- bs:
			case <-ctx.Done():
				err := ctx.Err()
				if err != nil {
					h.log.Error("SendWsWithCtx - error. subject:" + subject + ",error:" + err.Error())
				}
			case <-time.After(time.Duration(h.options.WriteTimeout)):
				close(client.send)
				delete(h.clients, client)
			}
		}
	})
}

func (h *Hubx) close() {
	h.log.Trace("close")
	//close hubx
	close(h.register)
	close(h.unregister)
	close(h.wsMessageChan)
	close(h.closeChan)
	h.ticker.Stop()
	h.unregister = nil
	h.register = nil
	h.wsMessageChan = nil
	h.closeChan = nil

	//close all clients
	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}

	// close broadcaster actor
	if h.broadcaster != nil {
		h.broadcaster.Close() <- true
		h.broadcaster = nil
	}

}

func (h *Hubx) callWithbcFilters(i int, msg PartialMessage) {
	if i == len(h.bcFilters) {
		h.onBcMessage(msg)
		return
	}
	h.bcFilters[i](msg, func() {
		h.callWithbcFilters(i+1, msg)
	})
}
func (h *Hubx) onBcMessage(msg PartialMessage) {
	h.log.Trace("onRMessage - subject:" + msg.Subject + " data:" + string(msg.Data))
	if cb, ok := h.BcListeners[msg.Subject]; ok {
		cb(msg)
	} else {
		h.log.Error("no handler for message subject:" + msg.Subject)
	}
}

func (h *Hubx) callWithWsFilters(i int, client *Client, msg PartialMessage) {
	h.log.Trace("callWithWsFilters - subject:" + msg.Subject + " data:" + string(msg.Data))
	if i == len(h.wsFilters) {
		h.onWsMessage(client, msg)
		return
	}
	h.wsFilters[i](client, msg, func() {
		h.callWithWsFilters(i+1, client, msg)
	})
}

func (h *Hubx) onWsMessage(client *Client, msg PartialMessage) {
	h.log.Trace("onWsMessage - subject:" + msg.Subject + " data:" + string(msg.Data))
	if cb, ok := h.listeners[msg.Subject]; ok {
		cb(client, msg)
	} else {
		h.log.Error("no handler for message subject:" + msg.Subject)
	}
}

func (h *Hubx) marshal(subject string, msg interface{}) ([]byte, error) {
	h.log.Trace("marshal - subject:" + subject)
	if data, ok := msg.([]byte); ok {
		return h.Marshaller(PartialMessage{Subject: subject, Data: data})
	} else {
		return h.Marshaller(Message{Subject: subject, Data: msg})
	}
}
