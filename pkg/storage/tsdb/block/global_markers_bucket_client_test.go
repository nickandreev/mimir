// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/storage/tsdb/bucketindex/markers_bucket_client_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package block

import (
	"bytes"
	"context"
	"path"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/mimir/pkg/storage/bucket"
	mimir_testutil "github.com/grafana/mimir/pkg/storage/tsdb/testutil"
)

func TestGlobalMarkersBucket_Delete_ShouldSucceedIfDeletionMarkDoesNotExistInTheBlockButExistInTheGlobalLocation(t *testing.T) {
	ctx := context.Background()

	// Create a mocked block deletion mark in the global location.
	blockID := ulid.MustNew(1, nil)
	for _, globalPath := range []string{DeletionMarkFilepath(blockID), NoCompactMarkFilepath(blockID)} {
		bkt, _ := mimir_testutil.PrepareFilesystemBucket(t)
		bkt = BucketWithGlobalMarkers(bkt)

		require.NoError(t, bkt.Upload(ctx, globalPath, strings.NewReader("{}")))

		// Ensure it exists before deleting it.
		ok, err := bkt.Exists(ctx, globalPath)
		require.NoError(t, err)
		require.True(t, ok)

		require.NoError(t, bkt.Delete(ctx, globalPath))

		// Ensure has been actually deleted.
		ok, err = bkt.Exists(ctx, globalPath)
		require.NoError(t, err)
		require.False(t, ok)
	}
}

func TestGlobalMarkersBucket_DeleteShouldDeleteGlobalMarkIfBlockMarkerDoesntExist(t *testing.T) {
	ctx := context.Background()

	blockID := ulid.MustNew(1, nil)

	for name, tc := range map[string]struct {
		blockMarker  string
		globalMarker string
	}{
		"deletion mark": {
			blockMarker:  path.Join(blockID.String(), DeletionMarkFilename),
			globalMarker: DeletionMarkFilepath(blockID),
		},
		"no compact": {
			blockMarker:  path.Join(blockID.String(), NoCompactMarkFilename),
			globalMarker: NoCompactMarkFilepath(blockID),
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Create a mocked block deletion mark in the global location.
			bkt, _ := mimir_testutil.PrepareFilesystemBucket(t)
			bkt = BucketWithGlobalMarkers(bkt)

			// Upload global only
			require.NoError(t, bkt.Upload(ctx, tc.globalMarker, strings.NewReader("{}")))

			// Verify global exists.
			verifyPathExists(t, bkt, tc.globalMarker, true)

			// Delete block marker.
			err := bkt.Delete(ctx, tc.blockMarker)
			require.NoError(t, err)

			// Ensure global one been actually deleted.
			verifyPathExists(t, bkt, tc.globalMarker, false)
		})
	}
}

func TestUploadToGlobalMarkerPath(t *testing.T) {
	blockID := ulid.MustNew(1, nil)
	for name, tc := range map[string]struct {
		blockMarker  string
		globalMarker string
	}{
		"deletion mark": {
			blockMarker:  path.Join(blockID.String(), DeletionMarkFilename),
			globalMarker: DeletionMarkFilepath(blockID),
		},
		"no compact": {
			blockMarker:  path.Join(blockID.String(), NoCompactMarkFilename),
			globalMarker: NoCompactMarkFilepath(blockID),
		},
	} {
		t.Run(name, func(t *testing.T) {
			bkt, _ := mimir_testutil.PrepareFilesystemBucket(t)
			bkt = BucketWithGlobalMarkers(bkt)

			// Verify that uploading block mark file uploads it to the global markers location too.
			require.NoError(t, bkt.Upload(context.Background(), tc.blockMarker, strings.NewReader("mark file")))

			verifyPathExists(t, bkt, tc.globalMarker, true)
		})
	}
}

func TestGlobalMarkersBucket_ExistShouldReportTrueOnlyIfBothExist(t *testing.T) {
	blockID := ulid.MustNew(1, nil)

	for name, tc := range map[string]struct {
		blockMarker  string
		globalMarker string
	}{
		"deletion mark": {
			blockMarker:  path.Join(blockID.String(), DeletionMarkFilename),
			globalMarker: DeletionMarkFilepath(blockID),
		},
		"no compact": {
			blockMarker:  path.Join(blockID.String(), NoCompactMarkFilename),
			globalMarker: NoCompactMarkFilepath(blockID),
		},
	} {
		t.Run(name, func(t *testing.T) {
			bkt, _ := mimir_testutil.PrepareFilesystemBucket(t)
			bkt = BucketWithGlobalMarkers(bkt)

			// Upload to global marker only
			require.NoError(t, bkt.Upload(context.Background(), tc.globalMarker, strings.NewReader("mark file")))

			// Verify global exists, but block marker doesn't.
			verifyPathExists(t, bkt, tc.globalMarker, true)
			verifyPathExists(t, bkt, tc.blockMarker, false)

			// Now upload to block marker (also overwrites global)
			require.NoError(t, bkt.Upload(context.Background(), tc.blockMarker, strings.NewReader("mark file")))

			// Verify global exists and block marker does too.
			verifyPathExists(t, bkt, tc.globalMarker, true)
			verifyPathExists(t, bkt, tc.blockMarker, true)

			// Now delete global file, and only keep block.
			require.NoError(t, bkt.Delete(context.Background(), tc.globalMarker))

			// Verify global doesn't exist anymore. Block marker also returns false, even though it *does* exist.
			verifyPathExists(t, bkt, tc.globalMarker, false)
			verifyPathExists(t, bkt, tc.blockMarker, false)
		})
	}
}

func verifyPathExists(t *testing.T, bkt objstore.Bucket, name string, expected bool) {
	t.Helper()

	ok, err := bkt.Exists(context.Background(), name)
	require.NoError(t, err)
	require.Equal(t, expected, ok)
}

func TestGlobalMarkersBucket_getGlobalMarkPathFromBlockMark(t *testing.T) {
	type testCase struct {
		name     string
		expected string
	}

	tests := []testCase{
		{name: "", expected: ""},
		{name: "01FV060K6XXCS8BCD2CH6C3GBR/index", expected: ""},
	}

	for _, marker := range []string{DeletionMarkFilename, NoCompactMarkFilename} {
		tests = append(tests, testCase{name: marker, expected: ""})
		tests = append(tests, testCase{name: "01FV060K6XXCS8BCD2CH6C3GBR/" + marker, expected: "markers/01FV060K6XXCS8BCD2CH6C3GBR-" + marker})
		tests = append(tests, testCase{name: "/path/to/01FV060K6XXCS8BCD2CH6C3GBR/" + marker, expected: "/path/to/markers/01FV060K6XXCS8BCD2CH6C3GBR-" + marker})
		tests = append(tests, testCase{name: "invalid-block-id/" + marker, expected: ""})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getGlobalMarkPathFromBlockMark(tc.name)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGlobalMarkersBucket_isDeletionMark(t *testing.T) {
	block1 := ulid.MustNew(1, nil)

	tests := []struct {
		name       string
		expectedOk bool
		expectedID ulid.ULID
	}{
		{
			name:       "",
			expectedOk: false,
		}, {
			name:       "deletion-mark.json",
			expectedOk: false,
		}, {
			name:       block1.String() + "/index",
			expectedOk: false,
		}, {
			name:       block1.String() + "/deletion-mark.json",
			expectedOk: true,
			expectedID: block1,
		}, {
			name:       "/path/to/" + block1.String() + "/deletion-mark.json",
			expectedOk: true,
			expectedID: block1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualID, actualOk := isDeletionMark(tc.name)
			assert.Equal(t, tc.expectedOk, actualOk)
			assert.Equal(t, tc.expectedID, actualID)
		})
	}
}

func TestGlobalMarkersBucket_isNoCompactMark(t *testing.T) {
	block1 := ulid.MustNew(1, nil)

	tests := []struct {
		name       string
		expectedOk bool
		expectedID ulid.ULID
	}{
		{
			name:       "",
			expectedOk: false,
		}, {
			name:       "no-compact-mark.json",
			expectedOk: false,
		}, {
			name:       block1.String() + "/index",
			expectedOk: false,
		}, {
			name:       block1.String() + "/no-compact-mark.json",
			expectedOk: true,
			expectedID: block1,
		}, {
			name:       "/path/to/" + block1.String() + "/no-compact-mark.json",
			expectedOk: true,
			expectedID: block1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualID, actualOk := isNoCompactMark(tc.name)
			assert.Equal(t, tc.expectedOk, actualOk)
			assert.Equal(t, tc.expectedID, actualID)
		})
	}
}

func TestBucketWithGlobalMarkers_ShouldWorkCorrectlyWithBucketMetrics(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	ctx := context.Background()

	// We wrap the underlying filesystem bucket client with metrics,
	// global markers (intentionally in the middle of the chain) and
	// user prefix.
	bkt, _ := mimir_testutil.PrepareFilesystemBucket(t)
	bkt = objstore.WrapWithMetrics(bkt, prometheus.WrapRegistererWithPrefix("thanos_", reg), "")
	bkt = BucketWithGlobalMarkers(bkt)
	userBkt := bucket.NewUserBucketClient("user-1", bkt, nil)

	reader, err := userBkt.Get(ctx, "does-not-exist")
	require.Error(t, err)
	require.Nil(t, reader)
	assert.True(t, bkt.IsObjNotFoundErr(err))

	// Should track the failure.
	assert.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
		# HELP thanos_objstore_bucket_operation_failures_total Total number of operations against a bucket that failed, but were not expected to fail in certain way from caller perspective. Those errors have to be investigated.
		# TYPE thanos_objstore_bucket_operation_failures_total counter
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="attributes"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="delete"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="exists"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="get"} 1
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="get_range"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="iter"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="upload"} 0
		# HELP thanos_objstore_bucket_operations_total Total number of all attempted operations against a bucket.
		# TYPE thanos_objstore_bucket_operations_total counter
		thanos_objstore_bucket_operations_total{bucket="",operation="attributes"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="delete"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="exists"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="get"} 1
		thanos_objstore_bucket_operations_total{bucket="",operation="get_range"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="iter"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="upload"} 0
	`),
		"thanos_objstore_bucket_operations_total",
		"thanos_objstore_bucket_operation_failures_total",
	))

	reader, err = userBkt.ReaderWithExpectedErrs(userBkt.IsObjNotFoundErr).Get(ctx, "does-not-exist")
	require.Error(t, err)
	require.Nil(t, reader)
	assert.True(t, bkt.IsObjNotFoundErr(err))

	// Should not track the failure.
	assert.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
		# HELP thanos_objstore_bucket_operation_failures_total Total number of operations against a bucket that failed, but were not expected to fail in certain way from caller perspective. Those errors have to be investigated.
		# TYPE thanos_objstore_bucket_operation_failures_total counter
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="attributes"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="delete"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="exists"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="get"} 1
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="get_range"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="iter"} 0
		thanos_objstore_bucket_operation_failures_total{bucket="",operation="upload"} 0
		# HELP thanos_objstore_bucket_operations_total Total number of all attempted operations against a bucket.
		# TYPE thanos_objstore_bucket_operations_total counter
		thanos_objstore_bucket_operations_total{bucket="",operation="attributes"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="delete"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="exists"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="get"} 2
		thanos_objstore_bucket_operations_total{bucket="",operation="get_range"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="iter"} 0
		thanos_objstore_bucket_operations_total{bucket="",operation="upload"} 0
	`),
		"thanos_objstore_bucket_operations_total",
		"thanos_objstore_bucket_operation_failures_total",
	))
}
