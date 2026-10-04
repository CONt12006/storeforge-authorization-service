package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/storeforge/authorization-service/internal/domain"
)

type AuthorizationRepository interface {
	Ping(context.Context) error
	GetEffectiveAccess(context.Context, int64) (domain.EffectiveAccess, error)
	GetUserRoles(context.Context, int64) ([]domain.Role, error)
	ReplaceUserRoles(context.Context, int64, []string) error
	ListRoles(context.Context) ([]domain.Role, error)
	CreateRole(context.Context, string) (domain.Role, error)
	AddPermissionToRole(context.Context, string, domain.AddPermissionRequest) error
	AddPolicyToRole(context.Context, string, domain.AddPolicyRequest) error
	Close()
}

type AccessCache interface {
	Get(context.Context, int64) (domain.EffectiveAccess, bool, error)
	Set(context.Context, int64, domain.EffectiveAccess, time.Duration) error
	Invalidate(context.Context, int64) error
	InvalidateAll(context.Context) error
	Ping(context.Context) error
	Close() error
}

type SessionValidator interface {
	Validate(context.Context, int64, string) (bool, error)
	Health(context.Context) error
}

type AuthorizationService struct {
	repository               AuthorizationRepository
	cache                    AccessCache
	sessionValidator         SessionValidator
	sessionValidationEnabled bool
	cacheTTL                 time.Duration
}

func NewAuthorizationService(
	repository AuthorizationRepository,
	cache AccessCache,
	sessionValidator SessionValidator,
	sessionValidationEnabled bool,
	cacheTTL time.Duration,
) *AuthorizationService {
	return &AuthorizationService{
		repository:               repository,
		cache:                    cache,
		sessionValidator:         sessionValidator,
		sessionValidationEnabled: sessionValidationEnabled,
		cacheTTL:                 cacheTTL,
	}
}

func (s *AuthorizationService) Authorize(ctx context.Context, request domain.AuthorizeRequest) (domain.Decision, error) {
	if err := s.validateRequest(request); err != nil {
		return domain.Decision{}, err
	}
	if s.sessionValidationEnabled {
		if request.SessionID == "" {
			return domain.Decision{Allowed: false, Reason: "session_required"}, nil
		}
		valid, err := s.sessionValidator.Validate(ctx, request.UserID, request.SessionID)
		if err != nil {
			return domain.Decision{}, fmt.Errorf("validate session: %w", err)
		}
		if !valid {
			return domain.Decision{Allowed: false, Reason: "invalid_session"}, nil
		}
	}
	access, err := s.effectiveAccess(ctx, request.UserID)
	if err != nil {
		return domain.Decision{}, err
	}
	policyDecision := s.evaluatePolicies(access.Policies, request)
	if policyDecision.Reason == "policy_denied" {
		return policyDecision, nil
	}
	if s.hasDirectPermission(access.Permissions, request.Resource.Type, request.Action) {
		return domain.Decision{Allowed: true, Reason: "permission_granted"}, nil
	}
	return policyDecision, nil
}

func (s *AuthorizationService) GetUserRoles(ctx context.Context, userID int64) ([]domain.Role, error) {
	if userID <= 0 {
		return nil, errors.New("user_id must be positive")
	}
	return s.repository.GetUserRoles(ctx, userID)
}

func (s *AuthorizationService) ReplaceUserRoles(ctx context.Context, userID int64, roles []string) error {
	if userID <= 0 {
		return errors.New("user_id must be positive")
	}
	for _, role := range roles {
		if strings.TrimSpace(role) == "" {
			return errors.New("role name must not be empty")
		}
	}
	if err := s.repository.ReplaceUserRoles(ctx, userID, roles); err != nil {
		return err
	}
	return s.cache.Invalidate(ctx, userID)
}

func (s *AuthorizationService) ListRoles(ctx context.Context) ([]domain.Role, error) {
	return s.repository.ListRoles(ctx)
}

func (s *AuthorizationService) CreateRole(ctx context.Context, name string) (domain.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Role{}, errors.New("role name must not be empty")
	}
	return s.repository.CreateRole(ctx, name)
}

func (s *AuthorizationService) AddPermissionToRole(ctx context.Context, roleName string, request domain.AddPermissionRequest) error {
	if strings.TrimSpace(roleName) == "" || strings.TrimSpace(request.Resource) == "" || strings.TrimSpace(request.Action) == "" {
		return errors.New("role, resource and action are required")
	}
	if err := s.repository.AddPermissionToRole(ctx, roleName, request); err != nil {
		return err
	}
	return s.cache.InvalidateAll(ctx)
}

func (s *AuthorizationService) AddPolicyToRole(ctx context.Context, roleName string, request domain.AddPolicyRequest) error {
	if strings.TrimSpace(roleName) == "" || strings.TrimSpace(request.Resource) == "" || strings.TrimSpace(request.Action) == "" {
		return errors.New("role, resource and action are required")
	}
	if request.Effect != "allow" && request.Effect != "deny" {
		return errors.New("effect must be allow or deny")
	}
	if request.ConditionType != "always" && request.ConditionType != "owner" {
		return errors.New("condition_type must be always or owner")
	}
	if err := s.repository.AddPolicyToRole(ctx, roleName, request); err != nil {
		return err
	}
	return s.cache.InvalidateAll(ctx)
}

func (s *AuthorizationService) Ready(ctx context.Context) error {
	if err := s.repository.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := s.cache.Ping(ctx); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if s.sessionValidationEnabled {
		if err := s.sessionValidator.Health(ctx); err != nil {
			return fmt.Errorf("session service: %w", err)
		}
	}
	return nil
}

func (s *AuthorizationService) effectiveAccess(ctx context.Context, userID int64) (domain.EffectiveAccess, error) {
	access, found, err := s.cache.Get(ctx, userID)
	if err == nil && found {
		return access, nil
	}
	access, err = s.repository.GetEffectiveAccess(ctx, userID)
	if err != nil {
		return domain.EffectiveAccess{}, err
	}
	if cacheErr := s.cache.Set(ctx, userID, access, s.cacheTTL); cacheErr != nil {
		return access, nil
	}
	return access, nil
}

func (s *AuthorizationService) hasDirectPermission(permissions []domain.Permission, resource, action string) bool {
	for _, permission := range permissions {
		resourceMatches := permission.Resource == "*" || permission.Resource == resource
		actionMatches := permission.Action == "*" || permission.Action == action
		if resourceMatches && actionMatches {
			return true
		}
	}
	return false
}

func (s *AuthorizationService) evaluatePolicies(policies []domain.Policy, request domain.AuthorizeRequest) domain.Decision {
	allowed := false
	for _, policy := range policies {
		if !s.policyMatches(policy, request) {
			continue
		}
		if policy.Effect == "deny" {
			return domain.Decision{Allowed: false, Reason: "policy_denied"}
		}
		if policy.Effect == "allow" {
			allowed = true
		}
	}
	if allowed {
		return domain.Decision{Allowed: true, Reason: "policy_granted"}
	}
	return domain.Decision{Allowed: false, Reason: "insufficient_permissions"}
}

func (s *AuthorizationService) policyMatches(policy domain.Policy, request domain.AuthorizeRequest) bool {
	resourceMatches := policy.Resource == "*" || policy.Resource == request.Resource.Type
	actionMatches := policy.Action == "*" || policy.Action == request.Action
	if !resourceMatches || !actionMatches {
		return false
	}
	switch policy.ConditionType {
	case "always":
		return true
	case "owner":
		return request.Resource.OwnerID != nil && *request.Resource.OwnerID == request.UserID
	default:
		return false
	}
}

func (s *AuthorizationService) validateRequest(request domain.AuthorizeRequest) error {
	if request.UserID <= 0 {
		return errors.New("user_id must be positive")
	}
	if strings.TrimSpace(request.Action) == "" {
		return errors.New("action is required")
	}
	if strings.TrimSpace(request.Resource.Type) == "" {
		return errors.New("resource.type is required")
	}
	return nil
}
