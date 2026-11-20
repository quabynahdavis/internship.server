package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"internship/server/internal/auth"
	appsqlite "internship/server/internal/sqlite"
)

type createSessionRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	if len(email) > maxEmailChars {
		auth.VerifyDummyPassword(req.Password)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	student, passwordHash, err := appsqlite.GetStudentByEmailForAuth(r.Context(), s.db, email)
	if err != nil {
		if errors.Is(err, appsqlite.ErrNotFound) {
			auth.VerifyDummyPassword(req.Password)
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		s.logger.Error("query student for login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := auth.VerifyPassword(passwordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	rawToken, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		s.logger.Error("generate session token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	expiresAt := s.now().UTC().Add(SessionTTL)
	if err := appsqlite.CreateSession(r.Context(), s.db, student.ID, tokenHash, expiresAt); err != nil {
		s.logger.Error("create session failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Token:     rawToken,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		Student:   student,
	})
}

func (s *server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := tokenHashFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := appsqlite.DeleteSession(r.Context(), s.db, tokenHash); err != nil {
		s.logger.Error("delete session failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
