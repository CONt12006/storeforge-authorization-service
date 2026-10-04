package domain

import "time"

type Resource struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	OwnerID *int64 `json:"owner_id,omitempty"`
}

type AuthorizeRequest struct {
	UserID    int64             `json:"user_id"`
	SessionID string            `json:"session_id,omitempty"`
	Action    string            `json:"action"`
	Resource  Resource          `json:"resource"`
	Context   map[string]string `json:"context,omitempty"`
}

type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type Role struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Permission struct {
	ID       int64  `json:"id"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type Policy struct {
	ID            int64  `json:"id"`
	RoleName      string `json:"role_name"`
	Resource      string `json:"resource"`
	Action        string `json:"action"`
	Effect        string `json:"effect"`
	ConditionType string `json:"condition_type"`
}

type EffectiveAccess struct {
	Permissions []Permission `json:"permissions"`
	Policies    []Policy     `json:"policies"`
}

type CreateRoleRequest struct {
	Name string `json:"name"`
}

type ReplaceUserRolesRequest struct {
	Roles []string `json:"roles"`
}

type AddPermissionRequest struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type AddPolicyRequest struct {
	Resource      string `json:"resource"`
	Action        string `json:"action"`
	Effect        string `json:"effect"`
	ConditionType string `json:"condition_type"`
}
