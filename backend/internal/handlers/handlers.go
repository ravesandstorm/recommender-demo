package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/embedclient"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/recsys/backend/internal/vector"
)

type API struct {
	Cfg    config.Config
	DB     *db.Store
	Redis  *redisstore.Store
	Qdrant *qdrantclient.Client
	Embed  *embedclient.Client
}

func (a *API) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", a.Health)
	r.Get("/api/config", a.GetConfig)
	r.Get("/api/users", a.ListUsers)
	r.Post("/api/users", a.CreateUser)

	r.Route("/api/posts/{postID}", func(r chi.Router) {
		r.Post("/like", a.Like)
		r.Delete("/like", a.Unlike)
		r.Post("/dislike", a.Dislike)
		r.Delete("/dislike", a.Undislike)
		r.Post("/save", a.Save)
		r.Delete("/save", a.Unsave)
		r.Post("/share", a.Share)
		r.Get("/comments", a.ListComments)
		r.Post("/comments", a.CreateComment)
	})

	r.Get("/api/feed", a.Feed)
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := r.Header.Get("X-User-ID")
	if raw == "" {
		writeErr(w, http.StatusUnauthorized, "missing X-User-ID header")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid X-User-ID")
		return uuid.Nil, false
	}
	ok, err := a.DB.UserExists(r.Context(), id)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "unknown user")
		return uuid.Nil, false
	}
	return id, true
}

func parsePostID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "postID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid post id")
		return uuid.Nil, false
	}
	return id, true
}

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) GetConfig(w http.ResponseWriter, r *http.Request) {
	weights, err := a.DB.ListWeights(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"feed_limit":        a.Cfg.FeedLimit,
		"qdrant_overfetch":  a.Cfg.QdrantOverfetch,
		"vector_dim":        a.Cfg.VectorDim,
		"interaction_weights": weights,
	})
}

func (a *API) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.DB.ListUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []db.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (a *API) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		writeErr(w, http.StatusBadRequest, "username required")
		return
	}
	u, err := a.DB.CreateUser(r.Context(), body.Username)
	if err != nil {
		writeErr(w, http.StatusConflict, "could not create user (username may be taken)")
		return
	}
	// init zero vector
	_ = a.Redis.SetUserVector(r.Context(), u.ID.String(), vector.Zero(a.Cfg.VectorDim))
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) applyVector(ctx context.Context, userID, postID uuid.UUID, interactionType string, subtract bool) error {
	weight, err := a.DB.GetWeight(ctx, interactionType)
	if err != nil {
		return err
	}
	postVec, err := a.Qdrant.GetVector(ctx, postID.String())
	if err != nil {
		return err
	}
	userVec, err := a.Redis.GetUserVector(ctx, userID.String())
	if err != nil {
		return err
	}
	if len(userVec) != a.Cfg.VectorDim {
		userVec = vector.Zero(a.Cfg.VectorDim)
	}
	var next []float32
	if subtract {
		next = vector.SubScaled(userVec, weight, postVec)
	} else {
		next = vector.AddScaled(userVec, weight, postVec)
	}
	return a.Redis.SetUserVector(ctx, userID.String(), next)
}

func (a *API) setLike(w http.ResponseWriter, r *http.Request, wantLike bool) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	prevLike, exists, err := a.DB.GetLike(ctx, userID, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Undo previous signal if switching or re-applying same after already set
	if exists {
		prevType := "dislike"
		if prevLike {
			prevType = "like"
		}
		if prevLike == wantLike {
			writeJSON(w, http.StatusOK, map[string]any{"status": "unchanged", "is_like": wantLike})
			return
		}
		if err := a.applyVector(ctx, userID, postID, prevType, true); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	newType := "dislike"
	if wantLike {
		newType = "like"
	}
	if err := a.DB.UpsertLike(ctx, userID, postID, wantLike); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.applyVector(ctx, userID, postID, newType, false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "is_like": wantLike})
}

func (a *API) clearLike(w http.ResponseWriter, r *http.Request, expectLike bool) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	prevLike, exists, err := a.DB.GetLike(ctx, userID, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists || prevLike != expectLike {
		writeJSON(w, http.StatusOK, map[string]string{"status": "noop"})
		return
	}
	typ := "dislike"
	if expectLike {
		typ = "like"
	}
	if err := a.applyVector(ctx, userID, postID, typ, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.DB.DeleteLike(ctx, userID, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *API) Like(w http.ResponseWriter, r *http.Request)    { a.setLike(w, r, true) }
func (a *API) Dislike(w http.ResponseWriter, r *http.Request) { a.setLike(w, r, false) }
func (a *API) Unlike(w http.ResponseWriter, r *http.Request)  { a.clearLike(w, r, true) }
func (a *API) Undislike(w http.ResponseWriter, r *http.Request) {
	a.clearLike(w, r, false)
}

func (a *API) Save(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	exists, err := a.DB.HasSave(ctx, userID, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unchanged"})
		return
	}
	if err := a.DB.InsertSave(ctx, userID, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.applyVector(ctx, userID, postID, "save", false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) Unsave(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	exists, err := a.DB.HasSave(ctx, userID, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		writeJSON(w, http.StatusOK, map[string]string{"status": "noop"})
		return
	}
	if err := a.applyVector(ctx, userID, postID, "save", true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.DB.DeleteSave(ctx, userID, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *API) Share(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	count, err := a.DB.IncrementShare(ctx, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.applyVector(ctx, userID, postID, "share", false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "share_count": count})
}

func (a *API) ListComments(w http.ResponseWriter, r *http.Request) {
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	comments, err := a.DB.ListComments(r.Context(), postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if comments == nil {
		comments = []db.Comment{}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (a *API) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	postID, ok := parsePostID(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Body == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	ctx := r.Context()
	c, err := a.DB.InsertComment(ctx, userID, postID, body.Body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.applyVector(ctx, userID, postID, "comment", false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) Feed(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	limit := a.Cfg.FeedLimit

	userVec, err := a.Redis.GetUserVector(ctx, userID.String())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var posts []db.Post
	if vector.IsZero(userVec) {
		posts, err = a.DB.RecentUnviewed(ctx, userID, limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		hits, err := a.Qdrant.Search(ctx, userVec, a.Cfg.QdrantOverfetch)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		candidateIDs := make([]uuid.UUID, 0, len(hits))
		for _, h := range hits {
			id, err := uuid.Parse(h.ID)
			if err != nil {
				continue
			}
			candidateIDs = append(candidateIDs, id)
		}
		filtered, err := a.DB.FilterUnviewed(ctx, userID, candidateIDs, limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		posts, err = a.DB.GetPostsByIDs(ctx, filtered)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		// backfill if vector search yielded too few after filtering
		if len(posts) < limit {
			need := limit - len(posts)
			extra, err := a.DB.RecentUnviewed(ctx, userID, need+len(posts))
			if err == nil {
				seen := map[uuid.UUID]struct{}{}
				for _, p := range posts {
					seen[p.ID] = struct{}{}
				}
				for _, p := range extra {
					if _, ok := seen[p.ID]; ok {
						continue
					}
					posts = append(posts, p)
					if len(posts) >= limit {
						break
					}
				}
			}
		}
	}

	ids := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	_ = a.DB.MarkViewed(ctx, userID, ids)

	type feedPost struct {
		ID      uuid.UUID `json:"id"`
		Title   string    `json:"title"`
		Content string    `json:"content"`
	}
	out := make([]feedPost, 0, len(posts))
	for _, p := range posts {
		out = append(out, feedPost{ID: p.ID, Title: p.Title, Content: p.Content})
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": out})
}
