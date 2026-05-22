package server

import (
	"context"
	"os"

	"github.com/oarkflow/authz"
	authzstores "github.com/oarkflow/authz/stores"
)

func AuthzEngineFromFile(path string) (*authz.Engine, error) {
	if path == "" {
		return DefaultAuthzEngine(), nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	policies := authzstores.NewMemoryPolicyStore()
	roles := authzstores.NewMemoryRoleStore()
	acls := authzstores.NewMemoryACLStore()
	audits := authzstores.NewMemoryAuditStore()
	members := authzstores.NewMemoryRoleMembershipStore()
	engine := authz.NewEngine(policies, roles, acls, audits, authz.WithRoleMembershipStore(members))
	cfg, err := authz.NewDSLParser().Parse(payload)
	if err != nil {
		return nil, err
	}
	if err := engine.ApplyConfig(context.Background(), cfg); err != nil {
		return nil, err
	}
	return engine, nil
}
