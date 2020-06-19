package hubx

import (
	"errors"

	"github.com/go-redis/redis/v7"
)

type RedisBroadcaster struct {
	send      chan []byte
	closeChan chan bool
	hub       *Hubx
	options   RedisOptions
	red       *redis.Client
	sub       *redis.PubSub
	log       *Logger
}

type RedisOptions struct {
	// Url         string
	Redis        redis.Options
	RedisChannel string //redis pubsub channel
	// PoolSize    int    //default is 2
	ChannelSize int
}

// func setRedisOptionsDefault(options *RedisOptions) {
// 	if options.PoolSize <= 0 {
// 		options.PoolSize = 2
// 	}
// 	if options.ChannelSize <= 0 {
// 		options.ChannelSize = 1
// 	}
// }

func NewRedisBroadcaster(hub *Hubx, options RedisOptions) (*RedisBroadcaster, error) {
	if options.RedisChannel == "" {
		return nil, errors.New("RedisBroadcaster's channel can not be empty.")
	}
	if options.ChannelSize <= 0 {
		options.ChannelSize = 1
	}
	r := &RedisBroadcaster{
		send:      make(chan []byte, options.ChannelSize),
		closeChan: make(chan bool),
		hub:       hub,
		options:   options,
	}
	r.log = NewLogger(hub.options.Logger, "github.com/RocksonZeta/hubx.RedisBroadcaster", hub.options.LoggerLevel)
	r.log.Trace("newRedisBroadcaster")
	err := r.initRedis()
	if err != nil {
		return nil, nil
	}
	return r, nil
}

func (r *RedisBroadcaster) initRedis() error {
	r.log.Trace("initRedis")
	// urlParts, err := url.Parse(r.options.Url)
	// if err != nil {
	// 	return err
	// }
	// pwd, _ := urlParts.User.Password()
	// path := urlParts.Path
	// path = strings.Trim(path, "/")
	// db, _ := strconv.Atoi(path)
	// r.red = redis.NewClient(&redis.Options{
	// 	Addr:         urlParts.Host,
	// 	DB:           db,
	// 	Password:     pwd,
	// 	PoolSize:     4, // one for pubsub , another for publish
	// 	MinIdleConns: 2,
	// })
	r.red = redis.NewClient(&r.options.Redis)
	r.sub = r.red.Subscribe(r.options.RedisChannel)
	_, err := r.sub.Receive()
	return err
}

func (r *RedisBroadcaster) Start() {
	Go(r.run)
}
func (r *RedisBroadcaster) Send() chan<- []byte {
	return r.send
}
func (r *RedisBroadcaster) Close() chan<- bool {
	return r.closeChan
}
func (r *RedisBroadcaster) run() {
	r.log.Trace("run")
	defer r.close()
	ch := r.sub.ChannelSize(r.options.ChannelSize)
	for {
		select {
		case msg := <-ch:
			r.log.Trace("receive from channel " + r.options.RedisChannel + " " + msg.String())
			r.hub.BroadcastMessage() <- []byte(msg.Payload)
			n := len(ch)
			for i := 0; i < n; i++ {
				r.hub.BroadcastMessage() <- []byte((<-ch).Payload)

			}
		case msg := <-r.send:
			r.log.Trace("send channel " + r.options.RedisChannel + " " + string(msg))
			err := r.red.Publish(r.options.RedisChannel, msg).Err()
			if err != nil {
				r.log.Error("publish message to channel " + r.options.RedisChannel + " error. " + err.Error())
			}
			n := len(r.send)
			for i := 0; i < n; i++ {
				err := r.red.Publish(r.options.RedisChannel, <-r.send).Err()
				if err != nil {
					r.log.Error("publish message to channel " + r.options.RedisChannel + " error. " + err.Error())
				}
			}
		case <-r.closeChan:
			r.log.Trace("closeChan")
			return
		}
	}
}

func (r *RedisBroadcaster) close() {
	r.log.Trace("close")
	r.sub.PUnsubscribe(r.options.RedisChannel)
	close(r.send)
	close(r.closeChan)
	r.sub.Close()
	r.red.Close()
}
