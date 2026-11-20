package httpapi

import (
	"errors"
	"net/http"
	"time"

	"internship/server/internal/auth"
	appsqlite "internship/server/internal/sqlite"
)

type onboardingRequest struct {
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	FullName    string   `json:"full_name"`
	Program     string   `json:"program"`
	YearOfStudy *int     `json:"year_of_study"`
	Institution *string  `json:"institution"`
	City        string   `json:"city"`
	Country     string   `json:"country"`
	StudentID   string   `json:"student_id"`
	NationalID  string   `json:"national_id"`
	Skills      []string `json:"skills"`
	Interests   []string `json:"interests"`
}

func (s *server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	var req onboardingRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	email, err := validateEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}
	if len(req.Password) < auth.MinPasswordBytes || len(req.Password) > auth.MaxPasswordBytes {
		writeError(w, http.StatusBadRequest, "password must be between 8 and 72 bytes")
		return
	}

	fullName, err := validateRequiredString("full_name", req.FullName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	program, err := validateRequiredString("program", req.Program)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	city, err := validateRequiredString("city", req.City)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	country, err := validateRequiredString("country", req.Country)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	studentID, err := validateRequiredString("student_id", req.StudentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nationalID, err := validateRequiredString("national_id", req.NationalID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.YearOfStudy != nil {
		if err := validateYearOfStudy(*req.YearOfStudy); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	var institution *string
	if req.Institution != nil {
		cleanedInst, err := validateRequiredString("institution", *req.Institution)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		institution = &cleanedInst
	}

	skills, err := validateStringList("skills", req.Skills)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	interests, err := validateStringList("interests", req.Interests)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	nationalIDHash, err := auth.HashNationalID(s.pepper, nationalID)
	if err != nil {
		s.logger.Error("hash national ID failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, "password must be between 8 and 72 bytes")
		return
	}

	student, err := appsqlite.CreateStudent(r.Context(), s.db, appsqlite.CreateStudentParams{
		Email:          email,
		PasswordHash:   passwordHash,
		FullName:       fullName,
		Program:        program,
		YearOfStudy:    req.YearOfStudy,
		Institution:    institution,
		City:           city,
		Country:        country,
		StudentID:      studentID,
		NationalIDHash: nationalIDHash,
		Skills:         skills,
		Interests:      interests,
	})
	if err != nil {
		if errors.Is(err, appsqlite.ErrDuplicateEmail) {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		if errors.Is(err, appsqlite.ErrDuplicateNationalID) {
			writeError(w, http.StatusConflict, "national_id already registered")
			return
		}
		s.logger.Error("create student failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	writeJSON(w, http.StatusCreated, authResponse{
		Token:     rawToken,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		Student:   student,
	})
}
