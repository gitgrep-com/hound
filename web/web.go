package web

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gitgrep-com/gitgrep/api"
	"github.com/gitgrep-com/gitgrep/config"
	"github.com/gitgrep-com/gitgrep/searcher"
	"github.com/gitgrep-com/gitgrep/ui"
)

// Server is an HTTP server that handles all
// http traffic for hound. It is able to serve
// some traffic before indexes are built and
// then transition to all traffic afterwards.
type Server struct {
	cfg *config.Config
	dev bool
	ch  chan error

	mux http.Handler
	lck sync.RWMutex
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == s.cfg.HealthCheckURI {
		fmt.Fprintln(w, "👍")
		return
	}

	s.lck.RLock()
	defer s.lck.RUnlock()
	if s.mux != nil {
		s.mux.ServeHTTP(w, r)
	} else {
		http.Error(w,
			"Gitgrep is not ready.",
			http.StatusServiceUnavailable)
	}
}

func (s *Server) serveWith(m http.Handler) {
	s.lck.Lock()
	defer s.lck.Unlock()
	s.mux = m
}

// Start creates a new server that will immediately start handling HTTP traffic.
// The HTTP server will return 200 on the health check, but a 503 on every other
// request until ServeWithIndex is called to begin serving search traffic with
// the given searchers.
func Start(cfg *config.Config, addr string, dev bool) *Server {
	ch := make(chan error)

	s := &Server{
		cfg: cfg,
		dev: dev,
		ch:  ch,
	}

	go func() {
		if cfg.FullCertFilename != "" && cfg.PrivCertFilename != "" {
			err := http.ListenAndServeTLS(addr, cfg.FullCertFilename, cfg.PrivCertFilename, s)
			if err != nil {
				log.Printf("ListenAndServeTLS %s: %v", addr, err)
			}
			ch <- err
		} else {
			err := http.ListenAndServe(addr, s)
			if err != nil {
				log.Printf("ListenAndServe %s: %v", addr, err)
			}
			ch <- err
		}
	}()

	return s
}

// ServeWithIndex allow the server to start offering the search UI and the
// search APIs operating on the given indexes.
func (s *Server) ServeWithIndex(idx map[string]*searcher.Searcher) error {
	h, err := ui.Content(s.dev, s.cfg)
	if err != nil {
		return err
	}

	m := http.NewServeMux()
	m.Handle("/", h)
	api.Setup(m, idx)

	s.serveWith(m)
	s = jwtCookieAuth(s)

	return <-s.ch
}
