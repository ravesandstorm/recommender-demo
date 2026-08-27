package testsuite

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/handlers"
)

// HandlersSuite returns tests verifying API routes, status codes, and feed partition logic.
func HandlersSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:        "Handlers/HealthEndpoint",
			Scope:       ScopeUnitHandlers,
			Description: "Asserts GET /health returns 200 OK with status: ok",
			Fn: func(tc *TestContext) {
				api := &handlers.API{}
				router := api.Routes()

				req := httptest.NewRequest(http.MethodGet, "/health", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				tc.AssertEqual(http.StatusOK, w.Code, "HTTP status code")
				var resp map[string]string
				err := json.NewDecoder(w.Body).Decode(&resp)
				tc.AssertNoError(err, "decode json")
				tc.AssertEqual("ok", resp["status"], "status payload")
			},
		},
		{
			Name:          "Handlers/ConfigSlotsVerification",
			Scope:         ScopeUnitHandlers,
			RequiresInfra: true,
			Description:   "Asserts GET /api/config returns active weights and partition slots",
			Fn: func(tc *TestContext) {
				api := &handlers.API{
					Cfg:   runner.GetConfig(),
					DB:    runner.GetDB(),
					Redis: runner.GetRedis(),
				}
				router := api.Routes()

				req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				tc.AssertEqual(http.StatusOK, w.Code, "HTTP status code")
				var cfgResp struct {
					FeedLimit  int            `json:"feed_limit"`
					VectorDim  int            `json:"vector_dim"`
					FeedSlots  map[string]int `json:"feed_slots"`
					Weights    map[string]float64 `json:"interaction_weights"`
				}
				err := json.NewDecoder(w.Body).Decode(&cfgResp)
				tc.AssertNoError(err, "decode config response")
				tc.Assert(cfgResp.FeedLimit > 0, "feed limit must be positive")
				tc.Assert(cfgResp.VectorDim == 384, "vector dim must be 384")
				tc.Assert(cfgResp.FeedSlots["exploit"] > 0, "exploit slot count must be positive")
				tc.Assert(cfgResp.FeedSlots["exploit"]+cfgResp.FeedSlots["soft_explore"]+cfgResp.FeedSlots["hard_explore"] == cfgResp.FeedLimit, "sum of slots must equal feed limit")
			},
		},
		{
			Name:        "Handlers/AuthHeaderEnforcement",
			Scope:       ScopeUnitHandlers,
			EdgeCase:    EdgeAuthValidation,
			Description: "Asserts protected endpoints reject missing or malformed X-User-ID headers",
			Fn: func(tc *TestContext) {
				api := &handlers.API{
					Cfg: runner.GetConfig(),
					DB:  runner.GetDB(),
				}
				router := api.Routes()

				// 1. Missing X-User-ID header
				reqMissing := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
				wMissing := httptest.NewRecorder()
				router.ServeHTTP(wMissing, reqMissing)
				tc.AssertEqual(http.StatusUnauthorized, wMissing.Code, "missing X-User-ID header status")

				// 2. Malformed X-User-ID header (not a valid UUID)
				reqMalformed := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
				reqMalformed.Header.Set("X-User-ID", "invalid-uuid-string")
				wMalformed := httptest.NewRecorder()
				router.ServeHTTP(wMalformed, reqMalformed)
				tc.AssertEqual(http.StatusBadRequest, wMalformed.Code, "malformed X-User-ID header status")

				// 3. Random non-existent UUID
				if runner.GetDB() != nil {
					nonExistentID := uuid.New().String()
					reqUnknown := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
					reqUnknown.Header.Set("X-User-ID", nonExistentID)
					wUnknown := httptest.NewRecorder()
					router.ServeHTTP(wUnknown, reqUnknown)
					tc.AssertEqual(http.StatusUnauthorized, wUnknown.Code, "unknown user X-User-ID status")
				}
			},
		},
		{
			Name:          "Handlers/CreateUserValidation",
			Scope:         ScopeUnitHandlers,
			RequiresInfra: true,
			Description:   "Asserts user creation validates empty username and succeeds with valid payload",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				api := &handlers.API{
					Cfg:   runner.GetConfig(),
					DB:    store,
					Redis: rdb,
				}
				router := api.Routes()

				// 1. Empty username payload
				emptyBody, _ := json.Marshal(map[string]string{"username": ""})
				reqEmpty := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(emptyBody))
				wEmpty := httptest.NewRecorder()
				router.ServeHTTP(wEmpty, reqEmpty)
				tc.AssertEqual(http.StatusBadRequest, wEmpty.Code, "empty username status")

				// 2. Valid username creation
				validName := "handler_user_" + uuid.New().String()[:8]
				validBody, _ := json.Marshal(map[string]string{"username": validName})
				reqValid := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(validBody))
				wValid := httptest.NewRecorder()
				router.ServeHTTP(wValid, reqValid)
				tc.AssertEqual(http.StatusCreated, wValid.Code, "create user status")

				var created db.User
				err := json.NewDecoder(wValid.Body).Decode(&created)
				tc.AssertNoError(err, "decode created user")
				tc.AssertEqual(validName, created.Username, "username match")

				// Cleanup
				_ = store.DeleteUser(tc.Context(), created.ID)
				_, _ = rdb.DeleteUserKeys(tc.Context(), created.ID.String())
			},
		},
	}
}
