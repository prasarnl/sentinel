package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"sentinel/server/internal/auth"
	"sentinel/server/internal/scripts"
	"sentinel/server/internal/spa"
)

func (s *Server) Router() (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"}, // Vite dev server
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key"},
		AllowCredentials: true,
	}))

	r.Get("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript")
		w.Write(scripts.InstallSh)
	})
	r.Get("/install.ps1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(scripts.InstallPs1)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.Login)
		r.Post("/auth/logout", s.Logout)
		r.Post("/enroll", s.Enroll)
		r.Post("/ingest", s.Ingest)

		// Public, unauthenticated: install scripts fetch prebuilt agent
		// binaries from here (analogous to fetching a public release asset).
		r.Handle("/downloads/*", http.StripPrefix("/api/v1/downloads/", http.FileServer(http.Dir(s.DownloadsDir))))

		r.Group(func(r chi.Router) {
			r.Use(s.Auth.RequireAuth)

			r.Get("/auth/me", s.Me)

			r.Get("/hosts", s.ListHosts)
			r.Get("/hosts/{id}/latest", s.LatestSnapshot)
			r.Get("/hosts/{id}/history/cpu", s.HistoryCPU)
			r.Get("/hosts/{id}/history/mem", s.HistoryMem)
			r.Get("/hosts/{id}/history/disk", s.HistoryDisk)
			r.Get("/hosts/{id}/history/gpu", s.HistoryGPU)
			r.Get("/hosts/{id}/stream", s.StreamHost)

			r.Get("/settings", s.GetSettings)

			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				r.Post("/hosts", s.CreateHost)
				r.Delete("/hosts/{id}", s.DeleteHost)
				r.Put("/settings", s.UpdateSettings)
				r.Get("/users", s.ListUsers)
				r.Post("/users", s.CreateUser)
				r.Patch("/users/{id}", s.UpdateUser)
				r.Delete("/users/{id}", s.DeleteUser)
			})
		})
	})

	spaHandler, err := spa.Handler()
	if err != nil {
		return nil, err
	}
	r.NotFound(spaHandler.ServeHTTP)

	return r, nil
}
