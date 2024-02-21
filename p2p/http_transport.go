package p2p

import (
	"errors"
	"log"
	"net/http"
	"time"
)

type HTTPTransport struct {
	server *http.Server
	mux    *http.ServeMux
}

func NewHTTPTransport(addr string) *HTTPTransport {
	return &HTTPTransport{
		server: &http.Server{
			Addr:         addr,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		mux: http.NewServeMux(),
	}
}

func (h *HTTPTransport) AddHandler(pattern string, handler func(w http.ResponseWriter, r *http.Request)) {
	h.mux.HandleFunc(pattern, handler)
}

func (h *HTTPTransport) StartServer() {
	h.server.Handler = h.mux
	if err := h.server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server fails to listen and serve \n")
	}
}
