package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/embedclient"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/recsys/backend/internal/vector"
	"github.com/recsys/backend/internal/viewwriter"
	"golang.org/x/sync/errgroup"
)

type API struct {
	Cfg    config.Config
	DB     *db.Store
	Redis  *redisstore.Store
	Qdrant *qdrantclient.Client
	Embed  *embedclient.Client
	Views  *viewwriter.Writer
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
	exploit, soft, hard := feedSlots(a.Cfg.FeedLimit)
	writeJSON(w, http.StatusOK, map[string]any{
		"feed_limit":             a.Cfg.FeedLimit,
		"qdrant_overfetch":       a.Cfg.QdrantOverfetch,
		"vector_dim":             a.Cfg.VectorDim,
		"interaction_weights":    weights,
		"interest_k":             a.Cfg.InterestK,
		"interest_sim_threshold": a.Cfg.InterestSimThreshold,
		"mmr_lambda":             a.Cfg.MMRLambda,
		"mmr_candidate_cap":      a.Cfg.MMRCandidateCap,
		"interest_collinear_min": a.Cfg.InterestCollinearMin,
		"seen_ttl_seconds":       int(a.Cfg.SeenTTL.Seconds()),
		"seen_hydrate_limit":     a.Cfg.SeenHydrateLimit,
		"view_retain_limit":      a.Cfg.ViewRetainLimit,
		"share_memo_ttl_seconds": int(a.Cfg.ShareMemoTTL.Seconds()),
		"recent_cache_ttl_seconds": int(a.Cfg.RecentCacheTTL.Seconds()),
		"recent_cache_size":      a.Cfg.RecentCacheSize,
		"feed_slots": map[string]int{
			"exploit":      exploit,
			"soft_explore": soft,
			"hard_explore": hard,
		},
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
	// init empty multi-interest profile (+ blended zero vector for legacy readers)
	_ = a.Redis.SetUserInterests(r.Context(), u.ID.String(), nil)
	_ = a.Redis.SetUserVector(r.Context(), u.ID.String(), vector.Zero(a.Cfg.VectorDim))
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) applyVector(ctx context.Context, userID, postID uuid.UUID, interactionType string, subtract bool) error {
	weight, err := a.DB.GetWeight(ctx, interactionType)
	if err != nil {
		return err
	}
	if subtract {
		weight = -weight
	}
	postVec, err := a.Qdrant.GetVector(ctx, postID.String())
	if err != nil {
		return err
	}
	interests, err := a.Redis.GetUserInterests(ctx, userID.String())
	if err != nil {
		return err
	}
	next := vector.ApplyInterest(interests, postVec, weight, a.Cfg.InterestK, a.Cfg.InterestSimThreshold)
	if err := a.Redis.SetUserInterests(ctx, userID.String(), next); err != nil {
		return err
	}
	// Keep blended single vector in sync for debugging / legacy cold-start checks.
	return a.Redis.SetUserVector(ctx, userID.String(), vector.BlendInterests(next, a.Cfg.VectorDim))
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
	uid := userID.String()
	pid := postID.String()

	memoHit, err := a.Redis.HasShareMemo(ctx, uid, pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if memoHit {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unchanged"})
		return
	}

	inserted, err := a.DB.InsertShare(ctx, userID, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Memo whether newly inserted or already in PG (conflict).
	_ = a.Redis.SetShareMemo(ctx, uid, pid, a.Cfg.ShareMemoTTL)
	if !inserted {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unchanged"})
		return
	}

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

func appendUnique(dst []db.Post, extras []db.Post, limit int, seen map[uuid.UUID]struct{}) []db.Post {
	for _, p := range extras {
		if len(dst) >= limit {
			break
		}
		if _, ok := seen[p.ID]; ok {
			continue
		}
		seen[p.ID] = struct{}{}
		dst = append(dst, p)
	}
	return dst
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func stringsToUUIDs(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (a *API) ensureSeenLoaded(ctx context.Context, userID uuid.UUID) error {
	return a.Redis.EnsureSeen(ctx, userID.String(), a.Cfg.SeenTTL, func(ctx context.Context) ([]string, error) {
		ids, err := a.DB.ListRecentViewedPostIDs(ctx, userID, a.Cfg.SeenHydrateLimit)
		if err != nil {
			return nil, err
		}
		return uuidsToStrings(ids), nil
	})
}

func (a *API) filterUnseenUUIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	unseen, err := a.Redis.FilterUnseen(ctx, userID.String(), uuidsToStrings(ids))
	if err != nil {
		return nil, err
	}
	return stringsToUUIDs(unseen), nil
}

func (a *API) recentUnseenPosts(ctx context.Context, userID uuid.UUID, limit int) ([]db.Post, error) {
	if limit <= 0 {
		return nil, nil
	}
	// Over-fetch then filter in Redis to avoid Postgres anti-join on the hot path.
	fetch := limit * 4
	if fetch < a.Cfg.QdrantOverfetch {
		fetch = a.Cfg.QdrantOverfetch
	}
	cacheSize := a.Cfg.RecentCacheSize
	if cacheSize < fetch {
		cacheSize = fetch
	}

	var recent []db.Post
	cached, err := a.Redis.GetRecentIDs(ctx)
	if err == nil && len(cached) >= fetch {
		ids := stringsToUUIDs(cached)
		if len(ids) > fetch {
			ids = ids[:fetch]
		}
		recent, err = a.DB.GetPostsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
	}
	if len(recent) < fetch {
		recent, err = a.DB.RecentPosts(ctx, cacheSize)
		if err != nil {
			return nil, err
		}
		idStrs := make([]string, len(recent))
		for i, p := range recent {
			idStrs[i] = p.ID.String()
		}
		_ = a.Redis.SetRecentIDs(ctx, idStrs, cacheSize, a.Cfg.RecentCacheTTL)
		if len(recent) > fetch {
			recent = recent[:fetch]
		}
	}

	ids := make([]uuid.UUID, len(recent))
	byID := make(map[uuid.UUID]db.Post, len(recent))
	for i, p := range recent {
		ids[i] = p.ID
		byID[p.ID] = p
	}
	unseen, err := a.filterUnseenUUIDs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]db.Post, 0, limit)
	for _, id := range unseen {
		if len(out) >= limit {
			break
		}
		out = append(out, byID[id])
	}
	return out, nil
}

type scoredHit struct {
	id     uuid.UUID
	score  float64
	vector []float32
}

func (a *API) multiInterestCandidates(ctx context.Context, interests []vector.Interest) ([]scoredHit, error) {
	active := vector.ActiveInterests(interests)
	if len(active) == 0 {
		return nil, nil
	}

	best := map[uuid.UUID]scoredHit{}
	var mu sync.Mutex
	merge := func(hits []qdrantclient.SearchHit, weight float64) {
		mu.Lock()
		defer mu.Unlock()
		for _, h := range hits {
			id, err := uuid.Parse(h.ID)
			if err != nil {
				continue
			}
			rel := h.Score * weight
			if prev, ok := best[id]; ok && prev.score >= rel {
				continue
			}
			best[id] = scoredHit{id: id, score: rel, vector: h.Vector}
		}
	}

	if vector.ShouldCollapseInterests(active, a.Cfg.InterestCollinearMin) {
		q := active[0].Vector
		w := active[0].Weight
		if len(active) > 1 {
			q = vector.BlendInterests(active, a.Cfg.VectorDim)
			w = 0
			for _, it := range active {
				w += it.Weight
			}
			if w <= 0 {
				w = 1
			}
		}
		hits, err := a.Qdrant.Search(ctx, q, a.Cfg.QdrantOverfetch)
		if err != nil {
			return nil, err
		}
		merge(hits, w)
	} else {
		weights := make([]float64, len(active))
		for i, it := range active {
			weights[i] = it.Weight
		}
		perInterest := a.Cfg.QdrantOverfetch / len(active)
		minPer := a.Cfg.FeedLimit * 3
		if perInterest < minPer {
			perInterest = minPer
		}
		totalSlots := perInterest * len(active)
		if totalSlots < a.Cfg.QdrantOverfetch {
			totalSlots = a.Cfg.QdrantOverfetch
		}
		quotas := vector.AllocateQuotas(weights, totalSlots)

		g, gctx := errgroup.WithContext(ctx)
		for i, it := range active {
			if quotas[i] <= 0 || vector.IsZero(it.Vector) {
				continue
			}
			i, it := i, it
			g.Go(func() error {
				hits, err := a.Qdrant.Search(gctx, it.Vector, quotas[i])
				if err != nil {
					return err
				}
				merge(hits, it.Weight)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}
	}

	out := make([]scoredHit, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].score > out[i].score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (a *API) Feed(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	limit := a.Cfg.FeedLimit

	if err := a.ensureSeenLoaded(ctx, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	interests, err := a.Redis.GetUserInterests(ctx, userID.String())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Legacy fallback: single blended vector from before multi-interest.
	if !vector.HasInterests(interests) {
		userVec, err := a.Redis.GetUserVector(ctx, userID.String())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !vector.IsZero(userVec) {
			interests = []vector.Interest{{Vector: vector.L2Normalize(userVec), Weight: 1}}
		}
	}

	var posts []db.Post
	if !vector.HasInterests(interests) {
		posts, err = a.recentUnseenPosts(ctx, userID, limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(posts) < limit {
			extra, err := a.DB.RecentPosts(ctx, limit)
			if err == nil {
				seen := map[uuid.UUID]struct{}{}
				for _, p := range posts {
					seen[p.ID] = struct{}{}
				}
				posts = appendUnique(posts, extra, limit, seen)
			}
		}
	} else {
		hits, err := a.multiInterestCandidates(ctx, interests)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		candidateIDs := make([]uuid.UUID, 0, len(hits))
		hitByID := make(map[uuid.UUID]scoredHit, len(hits))
		for _, h := range hits {
			candidateIDs = append(candidateIDs, h.id)
			hitByID[h.id] = h
		}

		ranked, err := a.filterUnseenUUIDs(ctx, userID, candidateIDs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		exploitN, softN, hardN := feedSlots(limit)
		personalN := exploitN + softN

		mmrIn := make([]vector.MMRCandidate, 0, len(ranked))
		for _, id := range ranked {
			h, ok := hitByID[id]
			if !ok {
				continue
			}
			mmrIn = append(mmrIn, vector.MMRCandidate{
				ID:        id.String(),
				Relevance: h.score,
				Vector:    h.vector,
			})
		}

		used := make(map[uuid.UUID]struct{}, limit)
		selectedIDs := make([]uuid.UUID, 0, limit)
		if personalN <= 2 {
			selectedIDs = append(selectedIDs, pickFromFront(ranked, personalN, used)...)
		} else {
			capN := a.Cfg.MMRCandidateCap
			if capN < 1 {
				capN = 1
			}
			if len(mmrIn) > capN {
				mmrIn = mmrIn[:capN]
			}
			mmrOut := vector.MMR(mmrIn, personalN, a.Cfg.MMRLambda)
			for _, c := range mmrOut {
				id, err := uuid.Parse(c.ID)
				if err != nil {
					continue
				}
				used[id] = struct{}{}
				selectedIDs = append(selectedIDs, id)
			}
		}

		// Soft explore leftover: if MMR under-filled, pull from lower similarity band.
		if len(selectedIDs) < personalN {
			need := personalN - len(selectedIDs)
			selectedIDs = append(selectedIDs, pickSoftExplore(ranked, need, used)...)
		}

		annSet := make(map[uuid.UUID]struct{}, len(candidateIDs))
		for _, id := range candidateIDs {
			annSet[id] = struct{}{}
		}
		if hardN > 0 {
			recent, err := a.recentUnseenPosts(ctx, userID, a.Cfg.QdrantOverfetch)
			if err == nil {
				for _, p := range recent {
					if len(selectedIDs) >= limit {
						break
					}
					if _, ok := used[p.ID]; ok {
						continue
					}
					if _, inANN := annSet[p.ID]; inANN {
						continue
					}
					used[p.ID] = struct{}{}
					selectedIDs = append(selectedIDs, p.ID)
				}
			}
		}

		if len(selectedIDs) < limit {
			selectedIDs = append(selectedIDs, pickFromFront(ranked, limit-len(selectedIDs), used)...)
		}

		posts, err = a.DB.GetPostsByIDs(ctx, selectedIDs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		seen := map[uuid.UUID]struct{}{}
		for _, p := range posts {
			seen[p.ID] = struct{}{}
		}
		if len(posts) < limit {
			extra, err := a.recentUnseenPosts(ctx, userID, limit)
			if err == nil {
				posts = appendUnique(posts, extra, limit, seen)
			}
		}
		if len(posts) < limit {
			extra, err := a.DB.GetPostsByIDs(ctx, candidateIDs)
			if err == nil {
				posts = appendUnique(posts, extra, limit, seen)
			}
		}
		if len(posts) < limit {
			extra, err := a.DB.RecentPosts(ctx, limit)
			if err == nil {
				posts = appendUnique(posts, extra, limit, seen)
			}
		}
	}

	ids := make([]uuid.UUID, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	// Hot path: sync Redis seen. Durable PG write is async via bounded workers.
	_ = a.Redis.MarkSeen(ctx, userID.String(), uuidsToStrings(ids), a.Cfg.SeenTTL)
	if a.Views != nil {
		a.Views.Enqueue(userID, ids)
	}

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
