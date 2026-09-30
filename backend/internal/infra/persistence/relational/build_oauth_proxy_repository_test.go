package relational

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/egress"
	repositorypkg "github.com/chenyme/grok2api/backend/internal/repository"
)

func TestBuildOAuthProxyCRUD(t *testing.T) {
	ctx := context.Background()
	database, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "oauth-proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}

	createdNode, err := NewEgressRepository(database).CreateEgressNode(ctx, egress.Node{
		Name: "build-free", Scope: egress.ScopeBuild, Enabled: true,
	})
	if err != nil || createdNode.ID == 0 {
		t.Fatalf("seed build node = %+v, err = %v", createdNode, err)
	}

	repository := NewEgressRepository(database)
	created, err := repository.CreateBuildOAuthProxy(ctx, egress.BuildOAuthProxy{
		Name: "paid-us", EncryptedProxyURL: "encrypted-url", Enabled: true,
	})
	if err != nil || created.ID == 0 || created.Name != "paid-us" || !created.Enabled {
		t.Fatalf("create = %+v, err = %v", created, err)
	}
	listed, err := repository.ListBuildOAuthProxies(ctx)
	if err != nil || len(listed) != 1 || listed[0].EncryptedProxyURL != "encrypted-url" {
		t.Fatalf("list = %+v, err = %v", listed, err)
	}
	got, err := repository.GetBuildOAuthProxy(ctx, created.ID)
	if err != nil || got.Name != "paid-us" {
		t.Fatalf("get = %+v, err = %v", got, err)
	}
	created.Enabled = false
	updated, err := repository.UpdateBuildOAuthProxy(ctx, created)
	if err != nil || updated.Enabled {
		t.Fatalf("update = %+v, err = %v", updated, err)
	}
	_, err = repository.CreateBuildOAuthProxy(ctx, egress.BuildOAuthProxy{
		Name: "paid-us", EncryptedProxyURL: "encrypted-url-2", Enabled: true,
	})
	if !errors.Is(err, repositorypkg.ErrConflict) {
		t.Fatalf("duplicate name error = %v", err)
	}
	if err := repository.DeleteBuildOAuthProxy(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = repository.ListBuildOAuthProxies(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("after delete list = %+v, err = %v", listed, err)
	}
	if _, err := repository.GetBuildOAuthProxy(ctx, created.ID); !errors.Is(err, repositorypkg.ErrNotFound) {
		t.Fatalf("get deleted error = %v", err)
	}

	preserved, err := repository.GetEgressNode(ctx, createdNode.ID)
	if err != nil || preserved.Name != "build-free" || preserved.Scope != egress.ScopeBuild {
		t.Fatalf("existing build node was not preserved: %+v, err = %v", preserved, err)
	}
	for _, table := range []string{"egress_nodes", "egress_subscription_sources", "request_audits"} {
		var sql string
		if err := database.db.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&sql).Error; err != nil {
			t.Fatal(err)
		}
		if strings.Contains(sql, "grok_build_oauth") {
			t.Fatalf("table %s must not add grok_build_oauth: %s", table, sql)
		}
	}
	var oauthSQL string
	if err := database.db.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", "build_oauth_proxies").Scan(&oauthSQL).Error; err != nil {
		t.Fatal(err)
	}
	if oauthSQL == "" || !strings.Contains(oauthSQL, "encrypted_proxy_url") {
		t.Fatalf("build_oauth_proxies was not created: %s", oauthSQL)
	}
}
