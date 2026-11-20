package httpapi

import (
	"errors"
	"net/http"

	appsqlite "internship/server/internal/sqlite"
)

type patchMeRequest struct {
	Program     optional[string]   `json:"program"`
	YearOfStudy optional[int]      `json:"year_of_study"`
	Institution optional[string]   `json:"institution"`
	City        optional[string]   `json:"city"`
	Country     optional[string]   `json:"country"`
	Skills      optional[[]string] `json:"skills"`
	Interests   optional[[]string] `json:"interests"`
}

func (s *server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	student, ok := studentFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	writeJSON(w, http.StatusOK, student)
}

func (s *server) handlePatchMe(w http.ResponseWriter, r *http.Request) {
	student, ok := studentFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req patchMeRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	var params appsqlite.UpdateStudentParams

	if req.Program.Set {
		if !req.Program.Valid {
			writeError(w, http.StatusBadRequest, "program must not be null")
			return
		}
		program, err := validateRequiredString("program", req.Program.Value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Program = &program
	}

	if req.YearOfStudy.Set {
		if !req.YearOfStudy.Valid {
			params.ClearYearOfStudy = true
		} else {
			if err := validateYearOfStudy(req.YearOfStudy.Value); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			year := req.YearOfStudy.Value
			params.YearOfStudy = &year
		}
	}

	if req.Institution.Set {
		if !req.Institution.Valid {
			params.ClearInstitution = true
		} else {
			inst, err := validateRequiredString("institution", req.Institution.Value)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			params.Institution = &inst
		}
	}

	if req.City.Set {
		if !req.City.Valid {
			writeError(w, http.StatusBadRequest, "city must not be null")
			return
		}
		city, err := validateRequiredString("city", req.City.Value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.City = &city
	}

	if req.Country.Set {
		if !req.Country.Valid {
			writeError(w, http.StatusBadRequest, "country must not be null")
			return
		}
		country, err := validateRequiredString("country", req.Country.Value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Country = &country
	}

	if req.Skills.Set {
		if !req.Skills.Valid {
			writeError(w, http.StatusBadRequest, "skills must not be null")
			return
		}
		skills, err := validateStringList("skills", req.Skills.Value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Skills = &skills
	}

	if req.Interests.Set {
		if !req.Interests.Valid {
			writeError(w, http.StatusBadRequest, "interests must not be null")
			return
		}
		interests, err := validateStringList("interests", req.Interests.Value)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Interests = &interests
	}

	updated, err := appsqlite.UpdateStudent(r.Context(), s.db, student.ID, params)
	if err != nil {
		if errors.Is(err, appsqlite.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		s.logger.Error("update student failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
