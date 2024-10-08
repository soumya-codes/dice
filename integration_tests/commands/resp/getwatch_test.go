package resp

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/dicedb/dice/internal/clientio"
	redis "github.com/dicedb/go-dice"
	"gotest.tools/v3/assert"
)

type WatchSubscriber struct {
	client *redis.Client
	watch  *redis.WatchCommand
}

const getWatchKey = "getwatchkey"

type getWatchTestCase struct {
	key string
	val string
}

var getWatchTestCases = []getWatchTestCase{
	{getWatchKey, "value1"},
	{getWatchKey, "value2"},
	{getWatchKey, "value3"},
	{getWatchKey, "value4"},
}

func TestGETWATCH(t *testing.T) {
	publisher := getLocalConnection()
	subscribers := []net.Conn{getLocalConnection(), getLocalConnection(), getLocalConnection()}

	FireCommand(publisher, fmt.Sprintf("DEL %s", getWatchKey))

	defer func() {
		if err := publisher.Close(); err != nil {
			t.Errorf("Error closing publisher connection: %v", err)
		}
		for _, sub := range subscribers {
			//FireCommand(sub, fmt.Sprintf("GET.UNWATCH %s", fingerprint))
			time.Sleep(100 * time.Millisecond)
			if err := sub.Close(); err != nil {
				t.Errorf("Error closing subscriber connection: %v", err)
			}
		}
	}()

	// Fire a SET command to set a key
	res := FireCommand(publisher, fmt.Sprintf("SET %s %s", getWatchKey, "value"))
	assert.Equal(t, "OK", res)

	respParsers := make([]*clientio.RESPParser, len(subscribers))
	for i, subscriber := range subscribers {
		rp := fireCommandAndGetRESPParser(subscriber, fmt.Sprintf("GET.WATCH %s", getWatchKey))
		assert.Assert(t, rp != nil)
		respParsers[i] = rp

		v, err := rp.DecodeOne()
		assert.NilError(t, err)
		castedValue, ok := v.([]interface{})
		if !ok {
			t.Errorf("Type assertion to []interface{} failed for value: %v", v)
		}
		assert.Equal(t, 3, len(castedValue))
	}

	//	Fire updates to the key using the publisher, then check if the subscribers receive the updates in the push-response form (i.e. array of three elements, with third element being the value)
	for _, tc := range getWatchTestCases {
		res := FireCommand(publisher, fmt.Sprintf("SET %s %s", tc.key, tc.val))
		assert.Equal(t, "OK", res)

		for _, rp := range respParsers {
			v, err := rp.DecodeOne()
			assert.NilError(t, err)
			castedValue, ok := v.([]interface{})
			if !ok {
				t.Errorf("Type assertion to []interface{} failed for value: %v", v)
			}
			assert.Equal(t, 3, len(castedValue))
			assert.Equal(t, "GET", castedValue[0])
			assert.Equal(t, "1768826704", castedValue[1])
			assert.Equal(t, tc.val, castedValue[2])
		}
	}
}

func TestGETWATCHWithSDK(t *testing.T) {
	publisher := getLocalSdk()
	subscribers := []WatchSubscriber{{client: getLocalSdk()}, {client: getLocalSdk()}, {client: getLocalSdk()}}

	publisher.Del(context.Background(), getWatchKey)

	channels := make([]<-chan *redis.WMessage, len(subscribers))
	for i, subscriber := range subscribers {
		watch := subscriber.client.WatchCommand(context.Background())
		subscribers[i].watch = watch
		assert.Assert(t, watch != nil)
		err := watch.Watch(context.Background(), "GET", getWatchKey)
		assert.NilError(t, err)
		channels[i] = watch.Channel()
		<-channels[i] // Get the first message
	}

	for _, tc := range getWatchTestCases {
		err := publisher.Set(context.Background(), tc.key, tc.val, 0).Err()
		assert.NilError(t, err)

		for _, channel := range channels {
			v := <-channel
			assert.Equal(t, "GET", v.Command)        // command
			assert.Equal(t, "1768826704", v.Name)    // Fingerprint
			assert.Equal(t, tc.val, v.Data.(string)) // data
		}
	}
}
