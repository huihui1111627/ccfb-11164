package server

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strconv"

	"spc/internal/core"
	"spc/internal/generator"
)

//go:embed dashboard.html
var dashboard []byte

type Server struct {
	e   *core.Engine
	g   *generator.Generator
	mux *http.ServeMux
}

func New(e *core.Engine, g *generator.Generator) *Server {
	s := &Server{e: e, g: g, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboard)
	})
	s.mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Overview())
	})
	s.mux.HandleFunc("/api/processes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Processes())
	})
	s.mux.HandleFunc("/api/characteristics", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Characteristics())
	})
	s.mux.HandleFunc("/api/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Machines())
	})
	s.mux.HandleFunc("/api/chart", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		last, _ := strconv.Atoi(q.Get("last"))
		writeJSON(w, s.e.SeriesView(q.Get("char"), q.Get("machine"), last))
	})
	s.mux.HandleFunc("/api/alarms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Alarms(r.URL.Query().Get("status")))
	})
	s.mux.HandleFunc("/api/alarms/action", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Action string `json:"action"`
			By     string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := s.e.AddAction(req.ID, req.Action, req.By, s.g.SimTime()); err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("/api/alarms/close", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := s.e.CloseAlarm(req.ID, s.g.SimTime()); err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("/api/rules", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CharID string  `json:"charId"`
			Rules  [8]bool `json:"rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, err)
			return
		}
		if err := s.e.SetRules(req.CharID, req.Rules); err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("/api/recalc", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CharID    string `json:"charId"`
			MachineID string `json:"machineId"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, err)
			return
		}
		if req.Reason == "" {
			req.Reason = "换模"
		}
		if err := s.e.Recalc(req.CharID, req.MachineID, req.Reason, s.g.SimTime()); err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("/api/recalc/events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.RecalcEvents())
	})
	s.mux.HandleFunc("/api/capability", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		by := q.Get("by")
		if by == "" {
			by = "batch"
		}
		writeJSON(w, s.e.Capability(q.Get("char"), q.Get("machine"), by))
	})
	s.mux.HandleFunc("/api/compare", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Compare(r.URL.Query().Get("char")))
	})
	s.mux.HandleFunc("/api/msa", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.g.RunMSA(r.URL.Query().Get("char"))
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, res)
	})
	s.mux.HandleFunc("/api/eval", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.g.Report())
	})
	s.mux.HandleFunc("/api/quality", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.e.Quality())
	})
	s.mux.HandleFunc("/api/time", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"simTime": s.g.SimTime().Format("2006-01-02 15:04:05")})
	})
}
