package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/storeforge/authorization-service/internal/domain"
	"github.com/storeforge/authorization-service/internal/service"
)

type Server struct {
	address        string
	internalAPIKey string
	service        *service.AuthorizationService
	httpServer     *http.Server
}

type errorResponse struct {
	Error string `json:"error"`
}

type rolesResponse struct {
	Roles []domain.Role `json:"roles"`
}

func New(address, internalAPIKey string, authorizationService *service.AuthorizationService) *Server {
	server := &Server{
		address:        address,
		internalAPIKey: internalAPIKey,
		service:        authorizationService,
	}
	server.httpServer = &http.Server{
		Addr:              address,
		Handler:           server.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", s.health)
	router.Get("/readyz", s.ready)
	router.Group(func(protected chi.Router) {
		protected.Use(s.internalAuth)
		protected.Post("/v1/authorize", s.authorize)
		protected.Get("/v1/roles", s.listRoles)
		protected.Post("/v1/roles", s.createRole)
		protected.Get("/v1/users/{userID}/roles", s.getUserRoles)
		protected.Put("/v1/users/{userID}/roles", s.replaceUserRoles)
		protected.Post("/v1/roles/{roleName}/permissions", s.addPermission)
		protected.Post("/v1/roles/{roleName}/policies", s.addPolicy)
	})
	return router
}

func (s *Server) internalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-Api-Key") != s.internalAPIKey {
			s.writeJSON(writer, http.StatusUnauthorized, errorResponse{Error: "invalid internal API key"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) health(writer http.ResponseWriter, request *http.Request) {
	s.writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := s.service.Ready(ctx); err != nil {
		s.writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) authorize(writer http.ResponseWriter, request *http.Request) {
	var payload domain.AuthorizeRequest
	if err := s.decode(request, &payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	decision, err := s.service.Authorize(request.Context(), payload)
	if err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.writeJSON(writer, http.StatusOK, decision)
}

func (s *Server) listRoles(writer http.ResponseWriter, request *http.Request) {
	roles, err := s.service.ListRoles(request.Context())
	if err != nil {
		s.writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	s.writeJSON(writer, http.StatusOK, rolesResponse{Roles: roles})
}

func (s *Server) createRole(writer http.ResponseWriter, request *http.Request) {
	var payload domain.CreateRoleRequest
	if err := s.decode(request, &payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	role, err := s.service.CreateRole(request.Context(), payload.Name)
	if err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.writeJSON(writer, http.StatusCreated, role)
}

func (s *Server) getUserRoles(writer http.ResponseWriter, request *http.Request) {
	userID, err := s.userID(request)
	if err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	roles, err := s.service.GetUserRoles(request.Context(), userID)
	if err != nil {
		s.writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	s.writeJSON(writer, http.StatusOK, rolesResponse{Roles: roles})
}

func (s *Server) replaceUserRoles(writer http.ResponseWriter, request *http.Request) {
	userID, err := s.userID(request)
	if err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	var payload domain.ReplaceUserRolesRequest
	if err := s.decode(request, &payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if err := s.service.ReplaceUserRoles(request.Context(), userID, payload.Roles); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) addPermission(writer http.ResponseWriter, request *http.Request) {
	var payload domain.AddPermissionRequest
	if err := s.decode(request, &payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if err := s.service.AddPermissionToRole(request.Context(), chi.URLParam(request, "roleName"), payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) addPolicy(writer http.ResponseWriter, request *http.Request) {
	var payload domain.AddPolicyRequest
	if err := s.decode(request, &payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if err := s.service.AddPolicyToRole(request.Context(), chi.URLParam(request, "roleName"), payload); err != nil {
		s.writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) userID(request *http.Request) (int64, error) {
	value := chi.URLParam(request, "userID")
	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || userID <= 0 {
		return 0, errors.New("invalid user id")
	}
	return userID, nil
}

func (s *Server) decode(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (s *Server) writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
