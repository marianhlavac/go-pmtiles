package pmtiles

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/stretchr/testify/assert"
)

func TestRelevantEntries(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 0, 0, 1})

	bitmap := roaring64.New()
	bitmap.Add(0)

	tiles, leaves := RelevantEntries(bitmap, 4, entries)

	assert.Equal(t, len(tiles), 1)
	assert.Equal(t, len(leaves), 0)
}

func TestRelevantEntriesRunLength(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 0, 0, 5})

	bitmap := roaring64.New()
	bitmap.Add(1)
	bitmap.Add(2)
	bitmap.Add(4)

	tiles, leaves := RelevantEntries(bitmap, 4, entries)

	assert.Equal(t, len(tiles), 2)
	assert.Equal(t, tiles[0].RunLength, uint32(2))
	assert.Equal(t, tiles[1].RunLength, uint32(1))
	assert.Equal(t, len(leaves), 0)
}

func TestRelevantEntriesLeaf(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 0, 0, 0})

	bitmap := roaring64.New()
	bitmap.Add(1)

	tiles, leaves := RelevantEntries(bitmap, 4, entries)

	assert.Equal(t, len(tiles), 0)
	assert.Equal(t, len(leaves), 1)
}

func TestRelevantEntriesNotLeaf(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 0, 0, 0})
	entries = append(entries, EntryV3{2, 0, 0, 1})
	entries = append(entries, EntryV3{4, 0, 0, 0})

	bitmap := roaring64.New()
	bitmap.Add(3)

	tiles, leaves := RelevantEntries(bitmap, 4, entries)

	assert.Equal(t, len(tiles), 0)
	assert.Equal(t, len(leaves), 0)
}

func TestRelevantEntriesMaxZoom(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 0, 0, 0})

	bitmap := roaring64.New()
	bitmap.Add(6)
	_, leaves := RelevantEntries(bitmap, 1, entries)
	assert.Equal(t, len(leaves), 0)

	_, leaves = RelevantEntries(bitmap, 2, entries)
	assert.Equal(t, len(leaves), 1)
}

func TestReencodeEntries(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 400, 10, 1})
	entries = append(entries, EntryV3{1, 500, 20, 2})

	reencoded, result, datalen, addressed, contents := reencodeEntries(entries)

	assert.Equal(t, 2, len(result))
	assert.Equal(t, result[0].SrcOffset, uint64(400))
	assert.Equal(t, result[0].Length, uint64(10))
	assert.Equal(t, result[1].SrcOffset, uint64(500))
	assert.Equal(t, result[1].Length, uint64(20))

	assert.Equal(t, 2, len(reencoded))
	assert.Equal(t, reencoded[0].Offset, uint64(0))
	assert.Equal(t, reencoded[1].Offset, uint64(10))

	assert.Equal(t, uint64(30), datalen)
	assert.Equal(t, uint64(3), addressed)
	assert.Equal(t, uint64(2), contents)
}

func TestReencodeEntriesDuplicate(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 400, 10, 1})
	entries = append(entries, EntryV3{1, 500, 20, 1})
	entries = append(entries, EntryV3{2, 400, 10, 1})

	reencoded, result, datalen, addressed, contents := reencodeEntries(entries)

	assert.Equal(t, 2, len(result))
	assert.Equal(t, result[0].SrcOffset, uint64(400))
	assert.Equal(t, result[0].Length, uint64(10))
	assert.Equal(t, result[1].SrcOffset, uint64(500))
	assert.Equal(t, result[1].Length, uint64(20))

	assert.Equal(t, len(reencoded), 3)
	assert.Equal(t, reencoded[0].Offset, uint64(0))
	assert.Equal(t, reencoded[1].Offset, uint64(10))
	assert.Equal(t, reencoded[2].Offset, uint64(0))

	assert.Equal(t, uint64(30), datalen)
	assert.Equal(t, uint64(3), addressed)
	assert.Equal(t, uint64(2), contents)
}

func TestReencodeContiguous(t *testing.T) {
	entries := make([]EntryV3, 0)
	entries = append(entries, EntryV3{0, 400, 10, 0})
	entries = append(entries, EntryV3{1, 410, 20, 0})

	_, result, _, _, _ := reencodeEntries(entries)

	assert.Equal(t, len(result), 1)
	assert.Equal(t, result[0].SrcOffset, uint64(400))
	assert.Equal(t, result[0].Length, uint64(30))
}

func TestMergeRanges(t *testing.T) {
	ranges := make([]srcDstRange, 0)
	ranges = append(ranges, srcDstRange{0, 0, 50})
	ranges = append(ranges, srcDstRange{60, 60, 60})

	result, totalTransferBytes := mergeRanges(ranges, 0.1)

	assert.Equal(t, 1, result.Len())
	assert.Equal(t, uint64(120), totalTransferBytes)
	front := result.Front().Value.(overfetchRange)
	assert.Equal(t, srcDstRange{0, 0, 120}, front.Rng)
	assert.Equal(t, 2, len(front.CopyDiscards))
	assert.Equal(t, copyDiscard{50, 10}, front.CopyDiscards[0])
	assert.Equal(t, copyDiscard{60, 0}, front.CopyDiscards[1])
}

func TestMergeRangesMultiple(t *testing.T) {
	ranges := make([]srcDstRange, 0)
	ranges = append(ranges, srcDstRange{0, 0, 50})
	ranges = append(ranges, srcDstRange{60, 60, 10})
	ranges = append(ranges, srcDstRange{80, 80, 10})

	result, totalTransferBytes := mergeRanges(ranges, 0.3)
	front := result.Front().Value.(overfetchRange)
	assert.Equal(t, uint64(90), totalTransferBytes)
	assert.Equal(t, 1, result.Len())
	assert.Equal(t, srcDstRange{0, 0, 90}, front.Rng)
	assert.Equal(t, 3, len(front.CopyDiscards))
}

func TestMergeRangesNonSrcOrdered(t *testing.T) {
	ranges := make([]srcDstRange, 0)
	ranges = append(ranges, srcDstRange{20, 0, 50})
	ranges = append(ranges, srcDstRange{0, 60, 50})

	result, _ := mergeRanges(ranges, 0.1)
	assert.Equal(t, 2, result.Len())
}

func TestParseCloudOutput(t *testing.T) {
	bucketURL, key, isCloud := parseCloudOutput("s3://mybucket/path/to/file.pmtiles")
	assert.True(t, isCloud)
	assert.Equal(t, "s3://mybucket", bucketURL)
	assert.Equal(t, "path/to/file.pmtiles", key)

	bucketURL, key, isCloud = parseCloudOutput("gs://mybucket/file.pmtiles")
	assert.True(t, isCloud)
	assert.Equal(t, "gs://mybucket", bucketURL)
	assert.Equal(t, "file.pmtiles", key)

	bucketURL, key, isCloud = parseCloudOutput("azblob://mycontainer/file.pmtiles")
	assert.True(t, isCloud)
	assert.Equal(t, "azblob://mycontainer", bucketURL)
	assert.Equal(t, "file.pmtiles", key)

	_, _, isCloud = parseCloudOutput("/local/path/file.pmtiles")
	assert.False(t, isCloud)

	_, _, isCloud = parseCloudOutput("-")
	assert.False(t, isCloud)
}

// TestExtractStreamingToStdout tests the streaming code path by capturing stdout.
// It clusters the unclustered fixture, extracts it to stdout, and verifies the result.
func TestExtractStreamingToStdout(t *testing.T) {
	ctx := context.Background()
	silentLogger := log.New(io.Discard, "", 0)

	// Extract requires a clustered source; cluster the fixture into a temp file.
	clusteredPath := makeFixtureCopy(t, "unclustered", "clustered_for_stream_test")
	err := Cluster(logger, clusteredPath, true)
	assert.NoError(t, err)

	// Capture stdout via a pipe
	origStdout := os.Stdout
	pr, pw, err := os.Pipe()
	assert.NoError(t, err)
	os.Stdout = pw

	extractErr := Extract(ctx, silentLogger, "", clusteredPath, -1, -1, "", "", "-", 1, 0.0, false)

	pw.Close()
	os.Stdout = origStdout

	assert.NoError(t, extractErr)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, pr)
	assert.NoError(t, err)
	pr.Close()

	// Write captured bytes to a temp file and verify the archive is valid
	outPath := filepath.Join(t.TempDir(), "out.pmtiles")
	err = os.WriteFile(outPath, buf.Bytes(), 0644)
	assert.NoError(t, err)

	err = Verify(silentLogger, outPath)
	assert.NoError(t, err)
}
