package router

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCaptureRoutesRequireRootSession(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousRateLimit := common.RedisEnabled, common.GlobalApiRateLimitEnable
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "capture-auth.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.Log{}, &model.AuditLog{}))
	require.NoError(t, i18n.Init())
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.GlobalApiRateLimitEnable = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.GlobalApiRateLimitEnable = previousRedis, previousRateLimit
		sqlDB, closeErr := db.DB()
		if assert.NoError(t, closeErr) {
			assert.NoError(t, sqlDB.Close())
		}
	})
	compressed := common.CompressText("upstream-secret: private response")
	other := model.NewLogOther()
	other.SetAdmin("anomaly", compressed)
	log := &model.Log{Other: other.JSONString()}
	require.NoError(t, db.Create(log).Error)
	engine := newAccessTokenScopeTestEngine(t)
	roles := []struct {
		name string
		role int
	}{
		{"root", common.RoleRootUser},
		{"root_pat", common.RoleRootUser},
		{"admin", common.RoleAdminUser},
		{"user", common.RoleCommonUser},
		{"anonymous", 0},
	}
	routes := []struct {
		method     string
		path       string
		rootStatus int
	}{
		{http.MethodGet, "/api/exception/capture?id=" + strconv.Itoa(log.Id), http.StatusOK},
		{http.MethodGet, "/api/exception/capture_size", http.StatusOK},
		{http.MethodGet, "/api/exception/capture_setting", http.StatusOK},
		{http.MethodPatch, "/api/exception/capture_setting", http.StatusBadRequest},
		{http.MethodPost, "/api/exception/clear_capture", http.StatusBadRequest},
	}
	for _, role := range roles {
		t.Run(role.name, func(t *testing.T) {
			var credential string
			if role.role != 0 {
				user := &model.User{Username: "capture-" + role.name, Password: "placeholder", Role: role.role, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: role.name}
				require.NoError(t, db.Create(user).Error)
				sid := "capture-route-" + role.name
				now := time.Now().Unix()
				require.NoError(t, model.CreateUserSession(&model.UserSession{SID: sid, UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "refresh-" + sid, LoginMethod: "password", LastActiveAt: now, ExpiresAt: now + 3600}))
				credential, _, err = service.IssueAccessToken(service.AuthIdentity{UserID: user.Id, SessionID: sid, UserAuthVersion: 1, SessionVersion: 1})
				require.NoError(t, err)
				if role.name == "root_pat" {
					suffix, err := common.GenerateRandomCharsKey(43)
					require.NoError(t, err)
					credential = model.AccessTokenPrefix + suffix
					token := &model.UserAccessToken{Name: "capture-test", TokenHash: model.AccessTokenFingerprint(credential), TokenHint: model.AccessTokenHint(credential), ExpiresAt: now + 3600}
					require.NoError(t, token.SetScopes([]string{"log:read"}))
					require.NoError(t, model.CreateUserAccessToken(user.Id, token, service.AccessTokenMaxPerUser))
				}
			}
			for _, route := range routes {
				t.Run(route.method+route.path, func(t *testing.T) {
					// 无效请求体让 root 到达参数校验，避免修改设置或清理数据。
					request := httptest.NewRequest(route.method, route.path, strings.NewReader("{"))
					request.Header.Set("Content-Type", "application/json")
					if credential != "" {
						request.Header.Set("Authorization", "Bearer "+credential)
					}
					response := httptest.NewRecorder()
					engine.ServeHTTP(response, request)
					want := http.StatusForbidden
					if role.name == "root" {
						want = route.rootStatus
					} else if role.role == 0 {
						want = http.StatusUnauthorized
					}
					assert.Equal(t, want, response.Code, response.Body.String())
					assert.NotContains(t, response.Body.String(), "upstream-secret")
					assert.NotContains(t, response.Body.String(), "private response")
					if role.name == "root" && strings.HasPrefix(route.path, "/api/exception/capture?") {
						var result struct {
							Data struct {
								Anomaly string `json:"anomaly"`
							} `json:"data"`
						}
						require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
						assert.Equal(t, compressed, result.Data.Anomaly, "抓包接口应原样返回压缩值")
						assert.Empty(t, response.Header().Get("Cache-Control"))
					} else if role.name != "root" {
						assert.NotContains(t, response.Body.String(), compressed)
					}
				})
			}
		})
	}
}

// accessTokenExemptRoutes are /api routes that never reach dashboard
// authentication, so a personal access token has no rule to satisfy there.
var accessTokenExemptRoutes = []string{
	// Anonymous pages and setup.
	"GET /api/setup",
	"POST /api/setup",
	"GET /api/status",
	"GET /api/uptime/status",
	"GET /api/notice",
	"GET /api/user-agreement",
	"GET /api/privacy-policy",
	"GET /api/about",
	"GET /api/home_page_content",
	"GET /api/ratio_config",
	"GET /api/user/groups",

	// Anonymous account entry points.
	"GET /api/verification",
	"GET /api/reset_password",
	"POST /api/user/reset",
	"POST /api/user/register",
	"GET /api/user/login/encryption-key",
	"POST /api/user/login",
	"POST /api/user/login/2fa",
	"POST /api/user/login/verify",
	"POST /api/user/login/passkey/begin",
	"POST /api/user/login/passkey/finish",
	"POST /api/user/passkey/login/begin",
	"POST /api/user/passkey/login/finish",
	"GET /api/oauth/wechat",
	"GET /api/oauth/telegram/login",
	"GET /api/oauth/telegram/bind/:flow_token",

	// Browser session cookie endpoints.
	"POST /api/user/auth/refresh",
	"POST /api/user/auth/logout",

	// Payment callbacks.
	"POST /api/stripe/webhook",
	"POST /api/creem/webhook",
	"POST /api/waffo/webhook",
	"POST /api/waffo-pancake/webhook/:env",
	"GET /api/user/epay/notify",
	"POST /api/user/epay/notify",
	"GET /api/subscription/epay/notify",
	"POST /api/subscription/epay/notify",
	"GET /api/subscription/epay/return",
	"POST /api/subscription/epay/return",

	// API key authentication (TokenAuthReadOnly).
	"GET /api/usage/token/",
	"GET /api/log/token",
}

// accessTokenHelperRoutes are declared through handlePermissionRoute outside
// channelPermissionRoutes.
var accessTokenHelperRoutes = map[string]authz.Permission{
	"GET /api/task_plugin_options": authz.TaskPluginBind,
	"GET /api/audit":               authz.AuditRead,
}

func newAccessTokenScopeTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// A permission route that is also in the static table panics here.
	require.NotPanics(t, func() {
		SetApiRouter(engine)
		SetDashboardRouter(engine)
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetVideoRouter(engine)
		SetTaskRouter(engine)
	})
	return engine
}

func TestAccessTokenRouteRulesCoverEveryDashboardRoute(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	exempt := map[string]bool{}
	for _, key := range accessTokenExemptRoutes {
		exempt[key] = true
	}
	registered := map[string]bool{}
	panel := map[string]bool{}
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/pg/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = true
		if !exempt[key] {
			panel[key] = true
		}
	}
	for _, key := range accessTokenExemptRoutes {
		assert.True(t, registered[key], "exempt route %s is no longer registered", key)
	}

	declared := map[string]int{}
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		declared[key]++
	}
	for key := range panel {
		assert.Equal(t, 1, declared[key], "panel route %s must be declared exactly once", key)
	}
	for key := range declared {
		assert.True(t, panel[key], "declared route %s is not a registered panel route", key)
	}

	permissionRoutes := map[string]authz.Permission{}
	for _, route := range channelPermissionRoutes {
		permissionRoutes[route.method+" "+joinPaths("/api/channel", route.path)] = route.permission
	}
	for key, permission := range accessTokenHelperRoutes {
		permissionRoutes[key] = permission
	}
	for key, permission := range permissionRoutes {
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		assert.Equal(t, "scope", rule.Kind(), key)
		assert.Equal(t, service.AccessTokenScopeOf(permission), rule.Scope(), key)
	}
}

func TestAccessTokenCatalogCoversEveryPermission(t *testing.T) {
	catalog := service.AccessTokenCatalog(1, common.RoleRootUser)
	scopes := map[string]bool{}
	resources := map[string]int{}
	for _, group := range catalog {
		for _, resource := range group.Resources {
			resources[resource.Resource]++
			for _, action := range resource.Actions {
				scopes[resource.Resource+":"+action.Action] = true
			}
		}
	}
	for _, permission := range authz.AllPermissions() {
		assert.True(t, scopes[service.AccessTokenScopeOf(permission)], "root catalog lacks %s", service.AccessTokenScopeOf(permission))
	}
	for resource, count := range resources {
		assert.Equal(t, 1, count, "resource %s appears in more than one catalog entry", resource)
	}
}

func TestAccessTokenVerificationScopesAreAllListed(t *testing.T) {
	files, err := filepath.Glob("../service/*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var scopes []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if !strings.HasPrefix(name.Name, "VerificationScope") || i >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					require.True(t, ok, name.Name)
					scope, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					scopes = append(scopes, scope)
				}
			}
		}
	}
	require.NotEmpty(t, scopes)
	for _, scope := range scopes {
		_, listed := service.AccessTokenVerificationScope(scope)
		assert.True(t, listed, "verification scope %s has no access token rule", scope)
	}
}
