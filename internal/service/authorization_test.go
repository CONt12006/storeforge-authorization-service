package service

import (
	"context"
	"testing"
	"time"

	"github.com/storeforge/authorization-service/internal/domain"
)

type testRepository struct {
	access domain.EffectiveAccess
	roles  []domain.Role
}

func (r *testRepository) Ping(context.Context) error { return nil }
func (r *testRepository) Close()                     {}
func (r *testRepository) GetEffectiveAccess(context.Context, int64) (domain.EffectiveAccess, error) {
	return r.access, nil
}
func (r *testRepository) GetUserRoles(context.Context, int64) ([]domain.Role, error) {
	return r.roles, nil
}
func (r *testRepository) ReplaceUserRoles(context.Context, int64, []string) error { return nil }
func (r *testRepository) ListRoles(context.Context) ([]domain.Role, error)        { return r.roles, nil }
func (r *testRepository) CreateRole(context.Context, string) (domain.Role, error) {
	return domain.Role{ID: 1, Name: "user"}, nil
}
func (r *testRepository) AddPermissionToRole(context.Context, string, domain.AddPermissionRequest) error {
	return nil
}
func (r *testRepository) AddPolicyToRole(context.Context, string, domain.AddPolicyRequest) error {
	return nil
}

type testCache struct {
	access domain.EffectiveAccess
	found  bool
}

func (c *testCache) Get(context.Context, int64) (domain.EffectiveAccess, bool, error) {
	return c.access, c.found, nil
}
func (c *testCache) Set(_ context.Context, _ int64, access domain.EffectiveAccess, _ time.Duration) error {
	c.access = access
	c.found = true
	return nil
}
func (c *testCache) Invalidate(context.Context, int64) error { c.found = false; return nil }
func (c *testCache) InvalidateAll(context.Context) error     { c.found = false; return nil }
func (c *testCache) Ping(context.Context) error              { return nil }
func (c *testCache) Close() error                            { return nil }

type testSessionValidator struct {
	valid bool
}

func (v *testSessionValidator) Validate(context.Context, int64, string) (bool, error) {
	return v.valid, nil
}
func (v *testSessionValidator) Health(context.Context) error { return nil }

func TestDirectPermission(t *testing.T) {
	repository := &testRepository{access: domain.EffectiveAccess{Permissions: []domain.Permission{{Resource: "order", Action: "read"}}}}
	service := NewAuthorizationService(repository, &testCache{}, &testSessionValidator{valid: true}, true, time.Minute)
	decision, err := service.Authorize(context.Background(), domain.AuthorizeRequest{
		UserID:    42,
		SessionID: "session-1",
		Action:    "read",
		Resource:  domain.Resource{Type: "order"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.Reason != "permission_granted" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestOwnerPolicy(t *testing.T) {
	ownerID := int64(42)
	repository := &testRepository{access: domain.EffectiveAccess{Policies: []domain.Policy{{Resource: "order", Action: "update", Effect: "allow", ConditionType: "owner"}}}}
	service := NewAuthorizationService(repository, &testCache{}, &testSessionValidator{valid: true}, false, time.Minute)
	decision, err := service.Authorize(context.Background(), domain.AuthorizeRequest{
		UserID:   42,
		Action:   "update",
		Resource: domain.Resource{Type: "order", ID: "100", OwnerID: &ownerID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.Reason != "policy_granted" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestInvalidSession(t *testing.T) {
	repository := &testRepository{access: domain.EffectiveAccess{Permissions: []domain.Permission{{Resource: "*", Action: "*"}}}}
	service := NewAuthorizationService(repository, &testCache{}, &testSessionValidator{valid: false}, true, time.Minute)
	decision, err := service.Authorize(context.Background(), domain.AuthorizeRequest{
		UserID:    42,
		SessionID: "revoked",
		Action:    "delete",
		Resource:  domain.Resource{Type: "order"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.Reason != "invalid_session" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
