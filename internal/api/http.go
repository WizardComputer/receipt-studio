package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"

	resources "receiptstudio.local/agent"
	"receiptstudio.local/agent/internal/model"
)

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) error {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		return errors.New("Content-Type mora biti application/json.")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("Neispravan JSON ili zahtjev veći od 1 MB.")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("Dozvoljen je jedan JSON objekt.")
	}
	return nil
}

func (a *server) localOrigin(origin string) bool {
	if origin == a.origin {
		return true
	}
	_, port, _ := net.SplitHostPort(a.host)
	scheme := strings.Split(a.origin, "://")[0]
	return origin == scheme+"://localhost:"+port
}

func equalToken(given, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(expected)) == 1
}

// Only integration endpoints are registered on the network listener.
func (a *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"status": "ready", "version": 1, "app_version": resources.AppVersion})
	})
	mux.HandleFunc("POST /v1/print-jobs", a.createJob)
	mux.HandleFunc("GET /v1/print-jobs/{id}", a.getJob)
	mux.HandleFunc("GET /v1/print-jobs/{id}/receipt", a.getReceipt)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, port, _ := net.SplitHostPort(a.host)
		if r.Host != a.host && r.Host != "localhost:"+port {
			a.backend.RecordDiagnostic("warning", "api", "Zahtjev je odbijen: nedozvoljen Host (HTTP 403).", "")
			problem(w, 403, "Nedozvoljen Host.")
			return
		}
		// No bootstrap, administration or UI assets are exposed over HTTP,
		// regardless of a caller's origin or credentials.
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		external := origin != "" && !a.localOrigin(origin)
		if external {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		if r.Method == "OPTIONS" {
			if external {
				w.WriteHeader(204)
			} else {
				a.backend.RecordDiagnostic("warning", "api", "Zahtjev je odbijen: nedozvoljen preflight (HTTP 403).", "")
				problem(w, 403, "Nedozvoljen preflight.")
			}
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		apiKey := a.backend.APIKey()
		if !equalToken(token, apiKey) {
			a.backend.RecordDiagnostic("warning", "api", "Zahtjev je odbijen: API ključ nije važeći (HTTP 401).", "")
			problem(w, http.StatusUnauthorized, "Potreban je važeći API ključ.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *server) createJob(w http.ResponseWriter, r *http.Request) {
	var req model.PrintRequest
	if err := readJSON(w, r, &req); err != nil {
		a.backend.RecordDiagnostic("warning", "api", "Zahtjev za printanje je odbijen: neispravan JSON, Content-Type ili veličina zahtjeva (HTTP 400).", "")
		problem(w, 400, err.Error())
		return
	}
	a.submit(w, req)
}

func (a *server) submit(w http.ResponseWriter, req model.PrintRequest) {
	job, duplicate, err := a.backend.Enqueue(req)
	if err != nil {
		status, level, component := 422, "warning", "api"
		message := "Zahtjev za printanje je odbijen: neispravni podaci ili postavke (HTTP 422)."
		if errors.Is(err, model.ErrConflict) {
			status = 409
			message = "Zahtjev je odbijen: isti job_id s različitim podacima ili printerom (HTTP 409)."
		}
		var failure *model.ActionError
		if errors.As(err, &failure) && failure.Kind == model.StorageFailure {
			status, level, component = 500, "error", "database"
			message = "Zahtjev za printanje nije sačuvan (HTTP 500)."
		}
		a.backend.RecordDiagnostic(level, component, message, "")
		problem(w, status, err.Error())
		return
	}
	status := 202
	if duplicate || req.ReturnPNG {
		status = 200
	}
	if duplicate {
		a.backend.RecordDiagnostic("info", "api", "Ponovljen zahtjev: vraćen je postojeći posao bez novog printanja.", job.ID)
	} else {
		a.backend.RecordDiagnostic("info", "api", "Zahtjev za printanje je prihvaćen.", job.ID)
	}
	response := model.JobSummary(job)
	response["duplicate"] = duplicate
	if req.ReturnPNG {
		response["png"] = job.PNG
	}
	respond(w, status, response)
}

func (a *server) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := a.backend.JobStatus(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Posao nije pronađen.")
		return
	}
	if err != nil {
		problem(w, 500, "Posao nije dostupan.")
		return
	}
	respond(w, 200, model.JobSummary(j))
}

func (a *server) getReceipt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !model.ValidID(id) {
		problem(w, http.StatusNotFound, "Posao nije pronađen.")
		return
	}
	receipt, err := a.backend.JobPreview(id)
	if err != nil {
		status := http.StatusInternalServerError
		var failure *model.ActionError
		if errors.As(err, &failure) {
			switch failure.Kind {
			case model.NotFound:
				status = http.StatusNotFound
			case model.InvalidReceipt:
				status = http.StatusUnprocessableEntity
			}
		}
		a.backend.RecordActionFailure("api", "Preuzimanje računa nije uspjelo.", err)
		problem(w, status, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(receipt.Data)))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": id + ".png"}))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(receipt.Data); err != nil {
		// The receipt/job remains durable; a failed download never reprints it.
		a.backend.RecordDiagnostic("warning", "api", "Preuzimanje računa je prekinuto tokom slanja odgovora.", id)
	}
}

type Service interface {
	APIKey() string
	Enqueue(model.PrintRequest) (*model.Job, bool, error)
	JobStatus(string) (*model.Job, error)
	JobPreview(string) (*model.ReceiptPNG, error)
	RecordDiagnostic(string, string, string, string)
	RecordActionFailure(string, string, error)
}
type server struct {
	backend      Service
	origin, host string
}

func New(backend Service, origin, host string) http.Handler {
	return (&server{backend: backend, origin: origin, host: host}).handler()
}
