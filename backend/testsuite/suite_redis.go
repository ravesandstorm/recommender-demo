package testsuite

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/vector"
)

// RedisSuite returns tests verifying hot caching, seen filters, and engagement state in Redis.
func RedisSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:          "Redis/VectorStorageAndZeroDefault",
			Scope:         ScopeRedisStore,
			RequiresInfra: true,
			Description:   "Asserts preference vector read/write and zero-vector fallback on key miss",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				defer func() { _, _ = rdb.DeleteUserKeys(ctx, uid) }()

				// 1. Missing vector key returns exact zero vector
				zeroVec, err := rdb.GetUserVector(ctx, uid)
				tc.AssertNoError(err, "get user vector on miss")
				tc.Assert(vector.IsZero(zeroVec), "missing key must return zero vector")
				tc.AssertEqual(runner.GetConfig().VectorDim, len(zeroVec), "vector dimension")

				// 2. Set and retrieve non-zero vector
				testVec := make([]float32, runner.GetConfig().VectorDim)
				testVec[0] = 1.23
				testVec[10] = -4.56
				err = rdb.SetUserVector(ctx, uid, testVec)
				tc.AssertNoError(err, "set user vector")

				gotVec, err := rdb.GetUserVector(ctx, uid)
				tc.AssertNoError(err, "get user vector after write")
				tc.Assert(!vector.IsZero(gotVec), "retrieved vector must not be zero")
				tc.AssertEqual(float32(1.23), gotVec[0], "vector[0] match")
				tc.AssertEqual(float32(-4.56), gotVec[10], "vector[10] match")
			},
		},
		{
			Name:          "Redis/MultiInterestJsonSerialization",
			Scope:         ScopeRedisStore,
			RequiresInfra: true,
			Description:   "Asserts multi-interest centroid list JSON marshaling and extraction",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				defer func() { _, _ = rdb.DeleteUserKeys(ctx, uid) }()

				interests := []vector.Interest{
					{Vector: vector.L2Normalize([]float32{1, 0, 0}), Weight: 2.5},
					{Vector: vector.L2Normalize([]float32{0, 1, 0}), Weight: 1.0},
				}
				err := rdb.SetUserInterests(ctx, uid, interests)
				tc.AssertNoError(err, "set user interests")

				got, err := rdb.GetUserInterests(ctx, uid)
				tc.AssertNoError(err, "get user interests")
				tc.AssertEqual(2, len(got), "interests length")
				tc.AssertEqual(2.5, got[0].Weight, "interest 0 weight")
				tc.AssertEqual(1.0, got[1].Weight, "interest 1 weight")
			},
		},
		{
			Name:          "Redis/SeenFilterAndHydration",
			Scope:         ScopeRedisStore,
			EdgeCase:      EdgeSeenHydrateFallback,
			RequiresInfra: true,
			Description:   "Asserts seen post filtering, TTL marking, and cold-start database hydration",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				defer func() {
					_ = rdb.DeleteSeenKey(ctx, uid)
					_, _ = rdb.DeleteUserKeys(ctx, uid)
				}()

				p1, p2, p3 := uuid.New().String(), uuid.New().String(), uuid.New().String()

				// 1. Initial hydration with empty callback
				err := rdb.EnsureSeen(ctx, uid, time.Hour, func(context.Context) ([]string, error) {
					return nil, nil
				})
				tc.AssertNoError(err, "ensure seen initial")

				// All 3 unseen
				unseen, err := rdb.FilterUnseen(ctx, uid, []string{p1, p2, p3})
				tc.AssertNoError(err, "filter unseen initial")
				tc.AssertEqual(3, len(unseen), "unseen count initially")

				// 2. Mark p2 as seen
				err = rdb.MarkSeen(ctx, uid, []string{p2}, time.Hour)
				tc.AssertNoError(err, "mark seen p2")

				unseen, err = rdb.FilterUnseen(ctx, uid, []string{p1, p2, p3})
				tc.AssertNoError(err, "filter unseen after mark")
				tc.AssertEqual(2, len(unseen), "unseen count after mark")
				tc.Assert(unseen[0] == p1 && unseen[1] == p3, "only p1 and p3 should be unseen")

				// 3. Delete seen key to simulate cache eviction & test hydrate callback
				_ = rdb.DeleteSeenKey(ctx, uid)
				err = rdb.EnsureSeen(ctx, uid, time.Hour, func(context.Context) ([]string, error) {
					return []string{p1}, nil // Hydrate p1 from DB
				})
				tc.AssertNoError(err, "ensure seen hydrate callback")

				unseen, err = rdb.FilterUnseen(ctx, uid, []string{p1, p2, p3})
				tc.AssertNoError(err, "filter unseen after hydration")
				tc.AssertEqual(2, len(unseen), "unseen count after hydration")
				tc.Assert(unseen[0] == p2 && unseen[1] == p3, "p1 was hydrated as seen")
			},
		},
		{
			Name:          "Redis/EngagementTombstoneResolution",
			Scope:         ScopeRedisStore,
			EdgeCase:      EdgeTombstoneResolution,
			RequiresInfra: true,
			Description:   "Asserts like/dislike state transitions and tombstone 'x' overrides",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				pid := uuid.New().String()
				defer func() { _, _ = rdb.DeleteUserKeys(ctx, uid) }()

				// 1. Set Like (1)
				err := rdb.SetEngLike(ctx, uid, pid, true)
				tc.AssertNoError(err, "set like")
				isLike, exists, err := rdb.GetEngLike(ctx, uid, pid)
				tc.AssertNoError(err, "get eng like")
				tc.Assert(exists, "must exist")
				tc.Assert(isLike, "must be like")

				// 2. Clear Like (sets tombstone 'x')
				err = rdb.ClearEngLike(ctx, uid, pid)
				tc.AssertNoError(err, "clear like")
				_, exists, err = rdb.GetEngLike(ctx, uid, pid)
				tc.AssertNoError(err, "get eng like after clear")
				tc.Assert(!exists, "cleared like must report not existing")

				rawVal, found, err := rdb.GetEngLikeField(ctx, uid, pid)
				tc.AssertNoError(err, "get raw field")
				tc.Assert(found, "field must be found in hash")
				tc.AssertEqual("x", rawVal, "tombstone marker 'x'")
			},
		},
		{
			Name:          "Redis/ShareMemoizationAndRecentCache",
			Scope:         ScopeRedisStore,
			EdgeCase:      EdgeShareMemoDeduplicate,
			RequiresInfra: true,
			Description:   "Asserts share memoization and recent posts LRU cache operations",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				pid := uuid.New().String()
				defer func() {
					_, _ = rdb.DeleteUserKeys(ctx, uid)
					_ = rdb.DeleteRecentKey(ctx)
				}()

				// 1. Share Memo
				hasShare, err := rdb.HasShareMemo(ctx, uid, pid)
				tc.AssertNoError(err, "has share memo initial")
				tc.Assert(!hasShare, "must be false initially")

				err = rdb.SetShareMemo(ctx, uid, pid, time.Hour)
				tc.AssertNoError(err, "set share memo")
				hasShare, err = rdb.HasShareMemo(ctx, uid, pid)
				tc.AssertNoError(err, "has share memo after set")
				tc.Assert(hasShare, "must be true after set")

				// 2. Recent Posts Cache
				pA, pB, pC := uuid.New().String(), uuid.New().String(), uuid.New().String()
				err = rdb.SetRecentIDs(ctx, []string{pA, pB}, 3, time.Hour)
				tc.AssertNoError(err, "set recent ids")

				err = rdb.PrependRecentID(ctx, pC, 3, time.Hour)
				tc.AssertNoError(err, "prepend recent id")

				recents, err := rdb.GetRecentIDs(ctx)
				tc.AssertNoError(err, "get recent ids")
				tc.AssertEqual(3, len(recents), "recent ids count")
				tc.AssertEqual(pC, recents[0], "prepended item must be front")
			},
		},
		{
			Name:          "Redis/DeleteUserKeysPurge",
			Scope:         ScopeRedisStore,
			EdgeCase:      EdgeCascadeUserCleanup,
			RequiresInfra: true,
			Description:   "Asserts DeleteUserKeys wipes all user hashes, vectors, interests, and seen sets",
			Fn: func(tc *TestContext) {
				rdb := runner.GetRedis()
				ctx := tc.Context()
				uid := uuid.New().String()
				pid := uuid.New().String()

				// Populate multiple keys for this user
				_ = rdb.SetUserVector(ctx, uid, vector.Zero(runner.GetConfig().VectorDim))
				_ = rdb.SetUserInterests(ctx, uid, []vector.Interest{{Vector: vector.Zero(3), Weight: 1}})
				_ = rdb.SetEngLike(ctx, uid, pid, true)
				_ = rdb.SetEngSave(ctx, uid, pid)
				_ = rdb.SetShareMemo(ctx, uid, pid, time.Hour)
				_ = rdb.MarkSeen(ctx, uid, []string{pid}, time.Hour)

				// Purge
				n, err := rdb.DeleteUserKeys(ctx, uid)
				tc.AssertNoError(err, "delete user keys")
				tc.Assert(n > 0, "must delete at least one key")

				// Verify clean state
				memo, _ := rdb.HasShareMemo(ctx, uid, pid)
				tc.Assert(!memo, "share memo must be gone")
				_, exists, _ := rdb.GetEngLike(ctx, uid, pid)
				tc.Assert(!exists, "engagement like must be gone")
			},
		},
	}
}
