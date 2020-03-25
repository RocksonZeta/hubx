package hubx

import (
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type RawMessage struct {
	Client *Client
	Msg    []byte
}

// const (
// 	// Time allowed to write a message to the peer.
// 	writeWait = 10 * time.Second

// 	// Time allowed to read the next pong message from the peer.
// 	pongWait = 60 * time.Second

// 	// Send pings to peer with this period. Must be less than pongWait.
// 	pingPeriod = (pongWait * 9) / 10

// 	// Maximum message size allowed from peer.
// 	maxMessageSize = 512
// )

// var (
// 	newline = []byte{'\n'}
// 	space   = []byte{' '}
// )

// var upgrader = websocket.Upgrader{
// 	ReadBufferSize:  1024,
// 	WriteBufferSize: 1024,
// }

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub   *Hubx
	conn  *websocket.Conn
	send  chan []byte
	Props sync.Map
	log   *Logger
}

func (c *Client) Send() chan<- []byte {
	return c.send
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	c.log.Trace("readPump")
	defer func() {
		c.log.Trace("readPump closed")
		if c.hub.unregister != nil {
			c.hub.unregister <- c
		}
		c.conn.Close()
	}()
	c.conn.SetReadLimit(c.hub.options.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.hub.options.PongTimeout))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(c.hub.options.PongTimeout)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
				c.hub.log.Error("hubx.Client.readPump error:" + err.Error())
			}
			break
		}
		// fmt.Println("client receive:", string(message))
		// message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		c.hub.wsMessageChan <- &RawMessage{Client: c, Msg: message}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	c.log.Trace("writePump")
	ticker := time.NewTicker(c.hub.options.PingPeriod)
	defer func() {
		c.log.Trace("writePump closed")
		if err := recover(); err != nil {
			if er, ok := err.(error); ok {
				c.log.Error("writePump error. " + er.Error())
			}
			debug.PrintStack()
		}
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.options.WriteTimeout))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				c.log.Error("writePump - get NextWriter error:" + err.Error())
				return
			}
			_, err = w.Write(message)
			if err != nil {
				c.log.Error("writePump - write msg error:" + err.Error())
				return
			}
			// Add queued chat messages to the current websocket message.
			// n := len(c.send)
			// for i := 0; i < n; i++ {
			// 	// w.Write(newline)
			// 	_, err = w.Write(<-c.send)
			// 	if err != nil {
			// 		c.log.Error("writePump - write msg error:" + err.Error())
			// 	}
			// }

			if err := w.Close(); err != nil {
				c.log.Error("writePump - writer close failed. error:" + err.Error())
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.options.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hubx, w http.ResponseWriter, r *http.Request, upgrader websocket.Upgrader, clientProps map[string]interface{}) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	log := NewLogger(hub.options.Logger, "github.com/RocksonZeta/hubx.Client", hub.options.LoggerLevel)

	client := &Client{hub: hub, conn: conn, send: make(chan []byte, hub.options.ChannelSize), log: log}
	if client.hub.register != nil {
		for k, v := range clientProps {
			client.Props.Store(k, v)
		}
		client.hub.register <- client
	} else {
		log.Error("ServeWs hubx is nil")
		return
	}

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	Go(client.writePump)
	Go(client.readPump)
	// go client.writePump()
	// go client.readPump()
}
func DefaultUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}
