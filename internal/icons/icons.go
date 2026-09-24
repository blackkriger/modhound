package icons

import (
	"net/http"
	"strings"
	"sync"
)

const prefix = "/icons/"

type icon struct {
	mime string
	data []byte
}

var store sync.Map

func Put(key, mime string, data []byte) string {
	store.Store(key, icon{mime: mime, data: data})
	return prefix + key
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := store.Load(strings.TrimPrefix(r.URL.Path, prefix))
		if !ok || !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		i := v.(icon)
		w.Header().Set("Content-Type", i.mime)
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(i.data)
	})
}
