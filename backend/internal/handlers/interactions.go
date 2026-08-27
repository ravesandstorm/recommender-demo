package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
)

// likeState prefers Redis engagement hash; falls back to Postgres on miss.
func (a *API) likeState(ctx context.Context, userID, postID uuid.UUID) (isLike bool, exists bool, err error) {
	val, found, err := a.Redis.GetEngLikeField(ctx, userID.String(), postID.String())
	if err != nil {
		return false, false, err
	}
	if found {
		if val == "x" {
			return false, false, nil
		}
		if val == "1" {
			return true, true, nil
		}
		if val == "0" {
			return false, true, nil
		}
		return false, false, nil
	}
	return a.DB.GetLike(ctx, userID, postID)
}

func (a *API) saveState(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	val, found, err := a.Redis.GetEngSaveField(ctx, userID.String(), postID.String())
	if err != nil {
		return false, err
	}
	if found {
		return val == "1", nil
	}
	return a.DB.HasSave(ctx, userID, postID)
}

func (a *API) ListMyInteractions(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	likes, err := a.DB.ListUserLikes(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dislikes, err := a.DB.ListUserDislikes(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	saves, err := a.DB.ListUserSaves(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	shares, err := a.DB.ListUserShares(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	comments, err := a.DB.ListUserComments(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	uid := userID.String()
	engLikes, err := a.Redis.AllEngLikes(ctx, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	engSaves, err := a.Redis.AllEngSaves(ctx, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	engShares, err := a.Redis.AllEngShares(ctx, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	likes, dislikes = mergeLikeLists(likes, dislikes, engLikes)
	saves = mergeActiveList(saves, engSaves)
	shares = mergeActiveList(shares, engShares)

	missing := collectMissingTitles(likes, dislikes, saves, shares)
	if len(missing) > 0 {
		titles, err := a.DB.GetPostTitles(ctx, missing)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		fillTitles(likes, titles)
		fillTitles(dislikes, titles)
		fillTitles(saves, titles)
		fillTitles(shares, titles)
	}

	if likes == nil {
		likes = []db.UserInteractionItem{}
	}
	if dislikes == nil {
		dislikes = []db.UserInteractionItem{}
	}
	if saves == nil {
		saves = []db.UserInteractionItem{}
	}
	if shares == nil {
		shares = []db.UserInteractionItem{}
	}
	if comments == nil {
		comments = []db.UserInteractionItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"likes":    likes,
		"dislikes": dislikes,
		"saves":    saves,
		"shares":   shares,
		"comments": comments,
	})
}

func mergeLikeLists(likes, dislikes []db.UserInteractionItem, eng map[string]string) ([]db.UserInteractionItem, []db.UserInteractionItem) {
	likeMap := indexByPost(likes)
	dislikeMap := indexByPost(dislikes)
	now := time.Now().UTC()

	for pid, val := range eng {
		id, err := uuid.Parse(pid)
		if err != nil {
			continue
		}
		switch val {
		case "1":
			delete(dislikeMap, id)
			if _, ok := likeMap[id]; !ok {
				likeMap[id] = db.UserInteractionItem{PostID: id, CreatedAt: now}
			}
		case "0":
			delete(likeMap, id)
			if _, ok := dislikeMap[id]; !ok {
				dislikeMap[id] = db.UserInteractionItem{PostID: id, CreatedAt: now}
			}
		case "x":
			delete(likeMap, id)
			delete(dislikeMap, id)
		}
	}
	return mapToSlice(likeMap), mapToSlice(dislikeMap)
}

func mergeActiveList(base []db.UserInteractionItem, eng map[string]string) []db.UserInteractionItem {
	m := indexByPost(base)
	now := time.Now().UTC()
	for pid, val := range eng {
		id, err := uuid.Parse(pid)
		if err != nil {
			continue
		}
		switch val {
		case "1":
			if _, ok := m[id]; !ok {
				m[id] = db.UserInteractionItem{PostID: id, CreatedAt: now}
			}
		case "x":
			delete(m, id)
		}
	}
	return mapToSlice(m)
}

func indexByPost(items []db.UserInteractionItem) map[uuid.UUID]db.UserInteractionItem {
	m := make(map[uuid.UUID]db.UserInteractionItem, len(items))
	for _, it := range items {
		m[it.PostID] = it
	}
	return m
}

func mapToSlice(m map[uuid.UUID]db.UserInteractionItem) []db.UserInteractionItem {
	out := make([]db.UserInteractionItem, 0, len(m))
	for _, it := range m {
		out = append(out, it)
	}
	return out
}

func collectMissingTitles(lists ...[]db.UserInteractionItem) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	var out []uuid.UUID
	for _, list := range lists {
		for _, it := range list {
			if it.Title != "" {
				continue
			}
			if _, ok := seen[it.PostID]; ok {
				continue
			}
			seen[it.PostID] = struct{}{}
			out = append(out, it.PostID)
		}
	}
	return out
}

func fillTitles(items []db.UserInteractionItem, titles map[uuid.UUID]string) {
	for i := range items {
		if items[i].Title == "" {
			items[i].Title = titles[items[i].PostID]
		}
	}
}
