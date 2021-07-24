package cache

import (
	"encoding/json"
	"net/http"

	"gopkg.in/mgo.v2/bson"
)

type ShowCache interface {
	Show(id bson.ObjectId) interface{}
	ShowAll() interface{}
}

func HandleDebugConfCache(cache ShowCache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var data []byte
		var err error
		key := r.URL.Query().Get("key")
		var bsonKey bson.ObjectId
		if bson.IsObjectIdHex(key) {
			bsonKey = bson.ObjectIdHex(key)
			data, err = json.Marshal(cache.Show(bsonKey))
		} else {
			data, err = json.Marshal(cache.ShowAll())
		}
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		} else {
			w.Write(data)
			return
		}

	})
}

type ShowBookChannelCache interface {
	Show(id string) interface{}
	ShowAll() interface{}
}

func HandleDebugBookChannelCache(cache ShowBookChannelCache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var data []byte
		var err error
		key := r.URL.Query().Get("key")
		if key != "" {
			data, err = json.Marshal(cache.Show(key))
		} else {
			data, err = json.Marshal(cache.ShowAll())
		}
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		} else {
			w.Write(data)
			return
		}

	})
}
