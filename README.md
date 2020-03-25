# hubx
golang webocket + redis  using actor + command pattern


## Install
```shell
go get -u github.com/RocksonZeta/hubx
```

## Example
```go
package hubx_test

import (
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/RocksonZeta/hubx"
)

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "play.html")
}

var h *hubx.Hubx

func newHub() *hubx.Hubx {
	fmt.Println("newHub")
	hub, err := hubx.New(hubx.Options{
		WriteTimeout: 3 * time.Second,
		Logger:       log.Writer(),
		ChannelSize:  100,
	})
	if err != nil {
		fmt.Println(err)
	}
	redisOptions := hubx.RedisOptions{
		Url:     "redis://:pwd@localhost:6379/1",
		Channel: "hello",
	}
	bs, err := hubx.NewRedisBroadcaster(hub, redisOptions)
	if err != nil {
		fmt.Println(err)
	}
	hub.SetBroadcast(bs)
	hub.AfterJoin = func(client *hubx.Client) {
		uid, _ := client.Props.Load("uid")
		fmt.Println("user join uid:" + uid.(string))
		return nil
	}
	hub.UseWs(func(client *hubx.Client, msg hubx.PartialMessage, next func()) {
		fmt.Println("before 1" + msg.String())
		next()
		fmt.Println("after 1" + msg.String())
	})
	hub.UseWs(func(client *hubx.Client, msg hubx.PartialMessage, next func()) {
		fmt.Println("before 2" + msg.String())
		next()
		fmt.Println("after 2" + msg.String())
	})
	hub.Use(func(msg hubx.PartialMessage, next func()) {
		fmt.Println("before r 1" + msg.String())
		next()
		fmt.Println("after r 1" + msg.String())
	})
	hub.OnWs("play", func(client *hubx.Client, msg hubx.PartialMessage) {
		fmt.Println("OnWs msg:" + msg.String())
		hub.Send(msg.Subject, msg.Data)
	})
	hub.On("play", func(msg hubx.PartialMessage) {
		fmt.Println("On msg:" + msg.String())
		hub.BroadcastWs(msg.Subject, msg.Data)
	})
	hub.OnWs("close", func(client *hubx.Client, msg hubx.PartialMessage) {
		fmt.Println("OnWs close:" + msg.String())
		hub.CloseAsync()
		h = nil
	})
	hub.Ticker = func() {
	}
	bs.Start()
	hub.Start()
	return hub
}

func TestMain(t *testing.T) {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if h == nil {
			h = newHub()
		}
		hubx.ServeWs(h, w, r, hubx.DefaultUpgrader(), map[string]interface{}{"uid": "1"})
	})
	err := http.ListenAndServe(":9000", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}


```