package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"sentinel/server/internal/auth"
	"sentinel/server/internal/models"
)

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id, username, role, created_at FROM users ORDER BY created_at`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()

	users := []models.User{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan user")
			return
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

type createUserRequest struct {
	Username string      `json:"username"`
	Password string      `json:"password"`
	Role     models.Role `json:"role"`
}

func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" || (req.Role != models.RoleAdmin && req.Role != models.RoleViewer) {
		writeError(w, http.StatusBadRequest, "username, password, and a valid role are required")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var u models.User
	err = s.Pool.QueryRow(r.Context(),
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
		 RETURNING id, username, role, created_at`,
		req.Username, hash, req.Role,
	).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	if err != nil {
		writeError(w, http.StatusConflict, "username already exists")
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

type updateUserRequest struct {
	Password *string      `json:"password,omitempty"`
	Role     *models.Role `json:"role,omitempty"`
}

func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		if _, err := s.Pool.Exec(r.Context(), `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update password")
			return
		}
	}
	if req.Role != nil {
		if *req.Role != models.RoleAdmin && *req.Role != models.RoleViewer {
			writeError(w, http.StatusBadRequest, "invalid role")
			return
		}
		if _, err := s.Pool.Exec(r.Context(), `UPDATE users SET role = $1 WHERE id = $2`, *req.Role, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update role")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	claims, _ := auth.FromContext(r.Context())
	if claims != nil && claims.UserID == id {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
