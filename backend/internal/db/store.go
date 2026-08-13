package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type Post struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	ShareCount int       `json:"share_count"`
	CreatedAt  time.Time `json:"created_at"`
}

type Comment struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	PostID    uuid.UUID `json:"post_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Username  string    `json:"username,omitempty"`
}

func (s *Store) CreateUser(ctx context.Context, username string) (User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO users (username) VALUES ($1)
		RETURNING id, username, created_at
	`, username).Scan(&u.ID, &u.Username, &u.CreatedAt)
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, username, created_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UserExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

func (s *Store) InsertPost(ctx context.Context, title, content string) (Post, error) {
	var p Post
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO posts (title, content) VALUES ($1, $2)
		RETURNING id, title, content, share_count, created_at
	`, title, content).Scan(&p.ID, &p.Title, &p.Content, &p.ShareCount, &p.CreatedAt)
	return p, err
}

func (s *Store) GetPostsByIDs(ctx context.Context, ids []uuid.UUID) ([]Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, title, content, share_count, created_at
		FROM posts WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[uuid.UUID]Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.ShareCount, &p.CreatedAt); err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	out := make([]Post, 0, len(ids))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) RecentUnviewed(ctx context.Context, userID uuid.UUID, limit int) ([]Post, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.title, p.content, p.share_count, p.created_at
		FROM posts p
		WHERE NOT EXISTS (
			SELECT 1 FROM post_views v WHERE v.user_id=$1 AND v.post_id=p.id
		)
		ORDER BY p.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.ShareCount, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RecentPosts(ctx context.Context, limit int) ([]Post, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.title, p.content, p.share_count, p.created_at
		FROM posts p
		ORDER BY p.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.ShareCount, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) FilterUnviewed(ctx context.Context, userID uuid.UUID, candidateIDs []uuid.UUID, limit int) ([]uuid.UUID, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id
		FROM unnest($2::uuid[]) WITH ORDINALITY AS c(id, ord)
		JOIN posts p ON p.id = c.id
		WHERE NOT EXISTS (
			SELECT 1 FROM post_views v WHERE v.user_id=$1 AND v.post_id=p.id
		)
		ORDER BY c.ord
		LIMIT $3
	`, userID, candidateIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MarkViewed inserts new view rows, increments posts.view_count for those inserts,
// then trims the user's post_views to at most retainLimit newest rows.
func (s *Store) MarkViewed(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID, retainLimit int) error {
	if len(postIDs) == 0 {
		return nil
	}
	if retainLimit < 1 {
		retainLimit = 1
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		INSERT INTO post_views (user_id, post_id)
		SELECT $1, x FROM unnest($2::uuid[]) AS x
		ON CONFLICT DO NOTHING
		RETURNING post_id
	`, userID, postIDs)
	if err != nil {
		return err
	}
	var inserted []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		inserted = append(inserted, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(inserted) > 0 {
		_, err = tx.Exec(ctx, `
			UPDATE posts SET view_count = view_count + 1
			WHERE id = ANY($1)
		`, inserted)
		if err != nil {
			return err
		}
	}

	var total int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM post_views WHERE user_id=$1
	`, userID).Scan(&total); err != nil {
		return err
	}
	if excess := total - retainLimit; excess > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM post_views
			WHERE ctid IN (
				SELECT ctid FROM post_views
				WHERE user_id=$1
				ORDER BY viewed_at ASC
				LIMIT $2
			)
		`, userID, excess)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// CountUserViews returns how many post_views rows exist for the user.
func (s *Store) CountUserViews(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM post_views WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

// GetPostViewCount returns posts.view_count for a post.
func (s *Store) GetPostViewCount(ctx context.Context, postID uuid.UUID) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT view_count FROM posts WHERE id=$1`, postID).Scan(&n)
	return n, err
}

func (s *Store) ListRecentViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT post_id
		FROM post_views
		WHERE user_id=$1
		ORDER BY viewed_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) GetWeight(ctx context.Context, interactionType string) (float64, error) {
	var w float64
	err := s.Pool.QueryRow(ctx, `SELECT weight FROM interaction_weights WHERE interaction_type=$1`, interactionType).Scan(&w)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("unknown interaction type %q", interactionType)
	}
	return w, err
}

func (s *Store) GetLike(ctx context.Context, userID, postID uuid.UUID) (isLike bool, exists bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT is_like FROM likes WHERE user_id=$1 AND post_id=$2`, userID, postID).Scan(&isLike)
	if err == pgx.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return isLike, true, nil
}

func (s *Store) UpsertLike(ctx context.Context, userID, postID uuid.UUID, isLike bool) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO likes (user_id, post_id, is_like) VALUES ($1,$2,$3)
		ON CONFLICT (user_id, post_id) DO UPDATE SET is_like=EXCLUDED.is_like, created_at=NOW()
	`, userID, postID, isLike)
	return err
}

func (s *Store) DeleteLike(ctx context.Context, userID, postID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM likes WHERE user_id=$1 AND post_id=$2`, userID, postID)
	return err
}

func (s *Store) HasSave(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saves WHERE user_id=$1 AND post_id=$2)`, userID, postID).Scan(&ok)
	return ok, err
}

func (s *Store) InsertSave(ctx context.Context, userID, postID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO saves (user_id, post_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING
	`, userID, postID)
	return err
}

func (s *Store) DeleteSave(ctx context.Context, userID, postID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM saves WHERE user_id=$1 AND post_id=$2`, userID, postID)
	return err
}

func (s *Store) InsertComment(ctx context.Context, userID, postID uuid.UUID, body string) (Comment, error) {
	var c Comment
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO comments (user_id, post_id, body) VALUES ($1,$2,$3)
		RETURNING id, user_id, post_id, body, created_at
	`, userID, postID, body).Scan(&c.ID, &c.UserID, &c.PostID, &c.Body, &c.CreatedAt)
	return c, err
}

func (s *Store) ListComments(ctx context.Context, postID uuid.UUID) ([]Comment, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.user_id, c.post_id, c.body, c.created_at, u.username
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.post_id=$1
		ORDER BY c.created_at ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.UserID, &c.PostID, &c.Body, &c.CreatedAt, &c.Username); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) InsertShare(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO shares (user_id, post_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING
	`, userID, postID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) IncrementShare(ctx context.Context, postID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, `
		UPDATE posts SET share_count = share_count + 1 WHERE id=$1
		RETURNING share_count
	`, postID).Scan(&count)
	return count, err
}

func (s *Store) ListWeights(ctx context.Context) (map[string]float64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT interaction_type, weight FROM interaction_weights`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var t string
		var w float64
		if err := rows.Scan(&t, &w); err != nil {
			return nil, err
		}
		out[t] = w
	}
	return out, rows.Err()
}
