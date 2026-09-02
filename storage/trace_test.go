// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/internal/testutil"
	"cloud.google.com/go/storage/internal"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"
	otcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
)

func TestStorageTraceStartEndSpan(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})

	// TODO: Remove setting development env var upon launch.
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	spanName := "storage.TestTrace.TestStartEndSpan"
	ctx, span := startSpan(ctx, spanName)
	newAttrs := attribute.Int("fakeKey", 800)
	span.SetAttributes(newAttrs)
	endSpan(ctx, nil)

	spans := te.Spans()
	gotSpan := spans[0]
	if len(spans) != 1 {
		t.Errorf("expected one span, got %d", len(spans))
	}
	if got, want := gotSpan.Name, appendPackageName(spanName); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	wantSpan := createWantSpanStub(spanName, getCommonAttributes())
	wantSpan.Attributes = append(wantSpan.Attributes, newAttrs)
	opts := []cmp.Option{
		cmp.Comparer(spanAttributesComparer),
	}
	if diff := testutil.Diff(gotSpan, wantSpan, opts...); diff != "" {
		t.Errorf("diff: -got, +want:\n%s\n", diff)
	}
}
func TestStorageTraceStartSpanOption(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})

	// TODO: Remove setting development env var upon launch.
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	spanName := "storage.TestTrace.TestStartSpanOption"
	attrMap := make(map[string]interface{})
	attrMap["my_string"] = "my string"
	attrMap["my_bool"] = true
	attrMap["my_int"] = 123
	attrMap["my_int64"] = int64(456)
	attrMap["my_float"] = 0.9
	spanStartOpts := makeSpanStartOptAttrs(attrMap)

	ctx, _ = startSpan(ctx, spanName, spanStartOpts...)
	endSpan(ctx, nil)

	spans := te.Spans()
	gotSpan := spans[0]
	if len(spans) != 1 {
		t.Errorf("expected one span, got %d", len(spans))
	}
	if got, want := gotSpan.Name, appendPackageName(spanName); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	wantSpan := createWantSpanStub(spanName, getCommonAttributes())
	wantSpan.Attributes = append(wantSpan.Attributes, otAttrs(attrMap)...)
	opts := []cmp.Option{
		cmp.Comparer(spanAttributesComparer),
	}
	if diff := testutil.Diff(gotSpan, wantSpan, opts...); diff != "" {
		t.Errorf("diff: -got, +want:\n%s\n", diff)
	}
}

func TestStorageTraceEndSpanRecordError(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})

	// TODO: Remove setting development env var upon launch.
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	spanName := "storage.TestTrace.TestRecordError"
	ctx, _ = startSpan(ctx, spanName)
	err := &googleapi.Error{Code: http.StatusBadRequest, Message: "INVALID ARGUMENT"}
	endSpan(ctx, err)

	spans := te.Spans()
	gotSpan := spans[0]
	if len(spans) != 1 {
		t.Errorf("expected one span, got %d", len(spans))
	}
	if got, want := gotSpan.Name, appendPackageName(spanName); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if want := otcodes.Error; gotSpan.Status.Code != want {
		t.Errorf("got %v, want %v", gotSpan.Status.Code, want)
	}
}

func createWantSpanStub(spanName string, attrs []attribute.KeyValue) tracetest.SpanStub {
	return tracetest.SpanStub{
		Name:       appendPackageName(spanName),
		Attributes: attrs,
		InstrumentationScope: instrumentation.Scope{
			Name:    "cloud.google.com/go/storage",
			Version: internal.Version,
		},
	}
}

func spanAttributesComparer(a, b tracetest.SpanStub) bool {
	if a.Name != b.Name {
		return false
	}
	if len(a.Attributes) != len(b.Attributes) {
		return false
	}
	if a.InstrumentationScope != b.InstrumentationScope {
		return false
	}
	return true
}

// makeSpanStartOptAttrs makes a SpanStartOption and converts a generic map to OpenTelemetry attributes.
func makeSpanStartOptAttrs(attrMap map[string]interface{}) []trace.SpanStartOption {
	attrs := otAttrs(attrMap)
	return []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
	}
}

// otAttrs converts a generic map to OpenTelemetry attributes.
func otAttrs(attrMap map[string]interface{}) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	for k, v := range attrMap {
		var a attribute.KeyValue
		switch v := v.(type) {
		case string:
			a = attribute.Key(k).String(v)
		case bool:
			a = attribute.Key(k).Bool(v)
		case int:
			a = attribute.Key(k).Int(v)
		case int64:
			a = attribute.Key(k).Int64(v)
		default:
			a = attribute.Key(k).String(fmt.Sprintf("%#v", v))
		}
		attrs = append(attrs, a)
	}
	return attrs
}

func TestStartSpanWithBucket(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})

	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	fetcher := &mockMetadataFetcher{
		fetchFunc: func(ctx context.Context, bucket string) (resource string, location string, err error) {
			return "projects/p1/buckets/" + bucket, "us-west1", nil
		},
	}

	tests := []struct {
		name         string
		bucket       string
		setupCache   func(*bucketMetadataCache)
		wantResource string
		wantLocation string
		verifyCache  bool
	}{
		{
			name:   "Cache Miss (Placeholder)",
			bucket: "bucket-miss",
			setupCache: func(c *bucketMetadataCache) {
				// empty cache
			},
			wantResource: "projects/_/buckets/bucket-miss",
			wantLocation: "global",
			verifyCache:  true,
		},
		{
			name:   "Cache Hit (Resolved)",
			bucket: "bucket-hit",
			setupCache: func(c *bucketMetadataCache) {
				c.put("bucket-hit", bucketMetadata{resource: "projects/p1/buckets/bucket-hit", location: "us-west1"})
			},
			wantResource: "projects/p1/buckets/bucket-hit",
			wantLocation: "us-west1",
			verifyCache:  false,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := newBucketMetadataCache(10, fetcher)
			tc.setupCache(cache)
			doneChan := make(chan struct{}, 1)
			if tc.verifyCache {
				cache.fetchDone = doneChan
			}
			client := &Client{bucketMetadataCache: cache}

			ctx1, _ := startSpanWithBucket(ctx, client, tc.bucket, "TestSpan")
			endSpan(ctx1, nil)

			spans := te.Spans()
			if len(spans) != i+1 {
				t.Fatalf("expected %d spans, got %d", i+1, len(spans))
			}
			gotSpan := spans[i]

			verifySpanAttributes(t, gotSpan, tc.wantResource, tc.wantLocation)

			if tc.verifyCache {
				// Wait for background fetch to complete and populate cache.
				select {
				case <-doneChan:
				case <-time.After(fetchBackgroundTimeout):
					t.Fatalf("timeout waiting for fetchBackground completion")
				}
				_, found := cache.get(tc.bucket)
				if !found {
					t.Fatalf("expected entry to be populated in cache")
				}
			}
		})
	}
}

func verifySpanAttributes(t *testing.T, span tracetest.SpanStub, wantResource, wantLocation string) {
	t.Helper()
	var gotResource, gotLocation string
	for _, attr := range span.Attributes {
		if attr.Key == "gcp.resource.destination.id" {
			gotResource = attr.Value.AsString()
		}
		if attr.Key == "gcp.resource.destination.location" {
			gotLocation = attr.Value.AsString()
		}
	}

	if gotResource != wantResource {
		t.Errorf("got resource %q, want %q", gotResource, wantResource)
	}

	if gotLocation != wantLocation {
		t.Errorf("got location %q, want %q", gotLocation, wantLocation)
	}
}

func TestEndSpanEviction(t *testing.T) {
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	bucketName := "evict-bucket"
	tests := []struct {
		name      string
		spanName  string
		err       error
		wantEvict bool
	}{
		{
			name:      "Evict on ErrBucketNotExist",
			spanName:  "Bucket.Attrs",
			err:       ErrBucketNotExist,
			wantEvict: true,
		},
		{
			name:      "Evict on googleapi.Error 404",
			spanName:  "Bucket.Attrs",
			err:       &googleapi.Error{Code: http.StatusNotFound},
			wantEvict: true,
		},
		{
			name:      "No Evict on 500",
			spanName:  "Bucket.Attrs",
			err:       &googleapi.Error{Code: http.StatusInternalServerError},
			wantEvict: false,
		},
		{
			name:      "No Evict on Object 404",
			spanName:  "Object.Attrs",
			err:       ErrObjectNotExist,
			wantEvict: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &mockMetadataFetcher{}
			cache := newBucketMetadataCache(10, fetcher)
			client := &Client{bucketMetadataCache: cache}

			// Populate cache.
			cache.put(bucketName, bucketMetadata{resource: "res", location: "loc"})

			ctx, _ := startSpanWithBucket(context.Background(), client, bucketName, tc.spanName)
			endSpan(ctx, tc.err)

			_, found := cache.get(bucketName)
			if tc.wantEvict && found {
				t.Errorf("expected bucket to be evicted")
			}
			if !tc.wantEvict && !found {
				t.Errorf("expected bucket to remain in cache")
			}
		})
	}
}

func TestRecordWriterTraceAttributes(t *testing.T) {
	testCases := []struct {
		name      string
		writer    *Writer
		wantAttrs map[string]interface{}
	}{
		{
			name: "resumable",
			writer: &Writer{
				ChunkSize:            256 * 1024,
				Append:               false,
				EnableParallelUpload: false,
				ObjectAttrs:          ObjectAttrs{Name: "test-file.txt"},
			},
			wantAttrs: map[string]interface{}{
				"gcp.storage.write.mode":   "resumable",
				"gcp.storage.payload.size": int64(256 * 1024),
				"gcp.storage.object.name":  "test-file.txt",
			},
		},
		{
			name: "oneshot",
			writer: &Writer{
				ChunkSize:            0,
				Append:               false,
				EnableParallelUpload: false,
				ObjectAttrs:          ObjectAttrs{Name: "test-oneshot.txt"},
			},
			wantAttrs: map[string]interface{}{
				"gcp.storage.write.mode":   "oneshot",
				"gcp.storage.payload.size": int64(0),
				"gcp.storage.object.name":  "test-oneshot.txt",
			},
		},
		{
			name: "oneshot_negative_chunk",
			writer: &Writer{
				ChunkSize:            -1,
				Append:               false,
				EnableParallelUpload: false,
				ObjectAttrs:          ObjectAttrs{Name: "test-oneshot-neg.txt"},
			},
			wantAttrs: map[string]interface{}{
				"gcp.storage.write.mode":   "oneshot",
				"gcp.storage.payload.size": int64(-1),
				"gcp.storage.object.name":  "test-oneshot-neg.txt",
			},
		},
		{
			name: "appendable",
			writer: &Writer{
				ChunkSize:            256 * 1024,
				Append:               true,
				EnableParallelUpload: false,
				ObjectAttrs:          ObjectAttrs{Name: "test-append.txt"},
			},
			wantAttrs: map[string]interface{}{
				"gcp.storage.write.mode":   "appendable",
				"gcp.storage.payload.size": int64(256 * 1024),
				"gcp.storage.object.name":  "test-append.txt",
			},
		},
		{
			name: "parallel",
			writer: &Writer{
				ChunkSize:            256 * 1024,
				Append:               false,
				EnableParallelUpload: true,
				ParallelUploadConfig: ParallelUploadConfig{
					PartSize:       16 * 1024 * 1024,
					MaxConcurrency: 4,
				},
				ObjectAttrs: ObjectAttrs{Name: "test-parallel.txt"},
			},
			wantAttrs: map[string]interface{}{
				"gcp.storage.write.mode":            "parallel",
				"gcp.storage.payload.size":          int64(256 * 1024),
				"gcp.storage.object.name":           "test-parallel.txt",
				"gcp.storage.parallel.part_size":   int64(16 * 1024 * 1024),
				"gcp.storage.parallel.concurrency": int64(4),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			te := testutil.NewOpenTelemetryTestExporter()
			t.Cleanup(func() {
				te.Unregister(ctx)
			})
			t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

			spanName := "Object.Writer"
			ctx, _ = startSpan(ctx, spanName)
			recordWriterTraceAttributes(ctx, tc.writer)
			endSpan(ctx, nil)

			spans := te.Spans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}
			gotSpan := spans[0]

			for k, wantVal := range tc.wantAttrs {
				found := false
				for _, a := range gotSpan.Attributes {
					if string(a.Key) == k {
						found = true
						switch v := wantVal.(type) {
						case string:
							if got := a.Value.AsString(); got != v {
								t.Errorf("key %q: got %v, want %v", k, got, v)
							}
						case int64:
							if got := a.Value.AsInt64(); got != v {
								t.Errorf("key %q: got %v, want %v", k, got, v)
							}
						case bool:
							if got := a.Value.AsBool(); got != v {
								t.Errorf("key %q: got %v, want %v", k, got, v)
							}
						}
					}
				}
				if !found {
					t.Errorf("attribute %q not found on span", k)
				}
			}
		})
	}
}

func TestRecordReaderTraceAttributes(t *testing.T) {
	testCases := []struct {
		name       string
		readMode   string
		offset     int64
		length     int64
		objectName string
		wantAttrs  map[string]interface{}
	}{
		{
			name:       "range",
			readMode:   "range",
			offset:     100,
			length:     500,
			objectName: "read-obj.txt",
			wantAttrs: map[string]interface{}{
				"gcp.storage.read.mode":      "range",
				"gcp.storage.payload.offset": int64(100),
				"gcp.storage.payload.size":   int64(500),
				"gcp.storage.object.name":    "read-obj.txt",
			},
		},
		{
			name:       "full",
			readMode:   "full",
			offset:     0,
			length:     -1,
			objectName: "read-full.txt",
			wantAttrs: map[string]interface{}{
				"gcp.storage.read.mode":   "full",
				"gcp.storage.object.name": "read-full.txt",
			},
		},
		{
			name:       "multi_range",
			readMode:   "multi_range",
			offset:     0,
			length:     0,
			objectName: "read-mrd.txt",
			wantAttrs: map[string]interface{}{
				"gcp.storage.read.mode":   "multi_range",
				"gcp.storage.object.name": "read-mrd.txt",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			te := testutil.NewOpenTelemetryTestExporter()
			t.Cleanup(func() {
				te.Unregister(ctx)
			})
			t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

			spanName := "Object.Reader"
			ctx, _ = startSpan(ctx, spanName)
			recordReaderTraceAttributes(ctx, tc.readMode, tc.offset, tc.length, tc.objectName)
			endSpan(ctx, nil)

			spans := te.Spans()
			if len(spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(spans))
			}
			gotSpan := spans[0]

			for k, wantVal := range tc.wantAttrs {
				found := false
				for _, a := range gotSpan.Attributes {
					if string(a.Key) == k {
						found = true
						switch v := wantVal.(type) {
						case string:
							if got := a.Value.AsString(); got != v {
								t.Errorf("key %q: got %v, want %v", k, got, v)
							}
						case int64:
							if got := a.Value.AsInt64(); got != v {
								t.Errorf("key %q: got %v, want %v", k, got, v)
							}
						}
					}
				}
				if !found {
					t.Errorf("attribute %q not found on span", k)
				}
			}
		})
	}
}

func TestStartChunkSpanWithChunkNumber(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	chunkCtx, _ := startChunkSpan(ctx, "Storage.UploadChunk", 262144, 262144, withChunkNumber(2))
	endSpan(chunkCtx, nil)

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	gotSpan := spans[0]
	if got, want := gotSpan.Name, "cloud.google.com/go/storage.Storage.UploadChunk"; got != want {
		t.Errorf("got span name %q, want %q", got, want)
	}

	wantAttrs := map[string]interface{}{
		"gcp.storage.chunk.number":   int64(2),
		"gcp.storage.payload.offset": int64(262144),
		"gcp.storage.payload.size":   int64(262144),
	}
	for k, wantVal := range wantAttrs {
		found := false
		for _, a := range gotSpan.Attributes {
			if string(a.Key) == k {
				found = true
				if got := a.Value.AsInt64(); got != wantVal.(int64) {
					t.Errorf("key %q: got %v, want %v", k, got, wantVal)
				}
			}
		}
		if !found {
			t.Errorf("attribute %q not found on span", k)
		}
	}
}

func TestWriterMarkClosedEndsChunkSpan(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	w := &Writer{
		ctx:       ctx,
		ChunkSize: 256 * 1024,
	}
	_, span := startSpan(ctx, "Storage.UploadChunk")
	w.curChunkSpan = span

	if err := w.markClosed(nil); err != nil {
		t.Fatalf("w.markClosed: %v", err)
	}
	if w.curChunkSpan != nil {
		t.Errorf("expected w.curChunkSpan to be nil after markClosed, got %v", w.curChunkSpan)
	}
}

func TestStartResumableInitSpan(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	initCtx, _ := startResumableInitSpan(ctx)
	endSpan(initCtx, nil)

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	gotSpan := spans[0]
	if got, want := gotSpan.Name, "cloud.google.com/go/storage.Storage.ResumableSessionInit"; got != want {
		t.Errorf("got span name %q, want %q", got, want)
	}
}

func TestStartChunkSpanDevTracingDisabled(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "false")

	chunkCtx, span := startChunkSpan(ctx, "Storage.UploadChunk", 0, 1024, withChunkNumber(1))
	endSpan(chunkCtx, nil)

	if span.SpanContext().IsValid() {
		t.Errorf("expected invalid span context when dev tracing is disabled, got %v", span.SpanContext())
	}
	spans := te.Spans()
	if len(spans) > 0 {
		t.Fatalf("expected 0 ended spans because dev tracing is disabled, but got %d ended spans: %v", len(spans), spans[0].Name)
	}
}

func TestDynamicSpanContext(t *testing.T) {
	type testKey struct{}
	baseCtx := context.WithValue(context.Background(), testKey{}, "initial-val")

	dynCtx, holder := newDynamicSpanContext(baseCtx)
	if got, want := dynCtx.Value(testKey{}), "initial-val"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	type secondKey struct{}
	updatedCtx := context.WithValue(baseCtx, secondKey{}, "updated-val")
	holder.Store(updatedCtx)

	if got, want := dynCtx.Value(secondKey{}), "updated-val"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := dynCtx.Value(testKey{}), "initial-val"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRecordObjectTraceAttributes(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	spanName := "Object.Attrs"
	ctx, _ = startSpan(ctx, spanName)
	recordObjectTraceAttributes(ctx, "data/file.txt")
	endSpan(ctx, nil)

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	gotSpan := spans[0]

	found := false
	for _, a := range gotSpan.Attributes {
		if string(a.Key) == "gcp.storage.object.name" {
			found = true
			if got := a.Value.AsString(); got != "data/file.txt" {
				t.Errorf("gcp.storage.object.name: got %v, want %v", got, "data/file.txt")
			}
		}
	}
	if !found {
		t.Errorf("attribute gcp.storage.object.name not found on span")
	}
}

func TestStartSpanWithBucketFromContext(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	cache := newBucketMetadataCache(10, &mockMetadataFetcher{})
	cache.put("my-bucket", bucketMetadata{
		resource: "projects/_/buckets/my-bucket",
		location: "us-central1",
	})
	ctx = context.WithValue(ctx, cacheContextKey, cache)

	ctx, _ = startSpanWithBucket(ctx, nil, "my-bucket", "grpcStorageClient.ObjectsListCall")
	endSpan(ctx, nil)

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	gotSpan := spans[0]

	wantAttrs := map[string]string{
		"gcp.resource.destination.id":       "projects/_/buckets/my-bucket",
		"gcp.resource.destination.location": "us-central1",
	}
	for k, v := range wantAttrs {
		found := false
		for _, a := range gotSpan.Attributes {
			if string(a.Key) == k && a.Value.AsString() == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("attribute %s=%s not found on span", k, v)
		}
	}
}

func TestRecordObjectTraceAttributes_EmptyAndDisabled(t *testing.T) {
	t.Run("empty object name", func(t *testing.T) {
		ctx := context.Background()
		te := testutil.NewOpenTelemetryTestExporter()
		t.Cleanup(func() {
			te.Unregister(ctx)
		})
		t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

		ctx, _ = startSpan(ctx, "Object.Attrs")
		recordObjectTraceAttributes(ctx, "")
		endSpan(ctx, nil)

		spans := te.Spans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}
		for _, a := range spans[0].Attributes {
			if string(a.Key) == "gcp.storage.object.name" {
				t.Errorf("expected no gcp.storage.object.name attribute for empty object name, but found one: %v", a.Value)
			}
		}
	})

	t.Run("dev tracing disabled", func(t *testing.T) {
		ctx := context.Background()
		t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "false")

		te := testutil.NewOpenTelemetryTestExporter()
		t.Cleanup(func() {
			te.Unregister(ctx)
		})

		ctx, span := tracer().Start(ctx, "Object.Attrs")
		recordObjectTraceAttributes(ctx, "file.txt")
		span.End()

		spans := te.Spans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}
		for _, a := range spans[0].Attributes {
			if string(a.Key) == "gcp.storage.object.name" {
				t.Errorf("expected no gcp.storage.object.name attribute when dev tracing is disabled, but found: %v", a.Value)
			}
		}
	})
}

func TestMetadataRetryBackoffTracing(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	// Test metadata operation (e.g. Bucket.Attrs or ObjectsListCall) experiencing 2 retries.
	spanName := "Bucket.Attrs"
	ctx, _ = startSpan(ctx, spanName)
	testInvID := "123e4567-e89b-12d3-a456-426614174000"
	recordRetryBackoffEvent(ctx, 1, time.Now().Add(-100*time.Millisecond), testInvID)
	recordRetryBackoffEvent(ctx, 2, time.Now().Add(-200*time.Millisecond), testInvID)
	endSpan(ctx, nil)

	spans := te.Spans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans (1 parent metadata span + 2 RetryBackoff child spans), got %d", len(spans))
	}

	var parentSpan tracetest.SpanStub
	var backoffCount int
	for _, s := range spans {
		if strings.HasSuffix(s.Name, "RetryBackoff") {
			backoffCount++
		} else if strings.HasSuffix(s.Name, spanName) {
			parentSpan = s
		}
	}
	if backoffCount != 2 {
		t.Errorf("expected 2 RetryBackoff child spans, got %d", backoffCount)
	}
	if parentSpan.Name == "" {
		t.Fatalf("failed to find parent metadata span %q", spanName)
	}
	if len(parentSpan.Events) != 2 {
		t.Fatalf("expected 2 gcp.storage.retry.backoff events on metadata span, got %d", len(parentSpan.Events))
	}
	for i, ev := range parentSpan.Events {
		if ev.Name != "gcp.storage.retry.backoff" {
			t.Errorf("event %d: got name %q, want %q", i, ev.Name, "gcp.storage.retry.backoff")
		}
		foundAttempt, foundInvID := false, false
		for _, a := range ev.Attributes {
			if string(a.Key) == "gcp.storage.retry.attempt" {
				foundAttempt = true
				if got, want := a.Value.AsInt64(), int64(i+1); got != want {
					t.Errorf("event %d: gcp.storage.retry.attempt = %d, want %d", i, got, want)
				}
			}
			if string(a.Key) == "gcp.storage.gccl-invocation-id" {
				foundInvID = true
				if got, want := a.Value.AsString(), "gccl-invocation-id/"+testInvID; got != want {
					t.Errorf("event %d: gcp.storage.gccl-invocation-id = %q, want %q", i, got, want)
				}
			}
		}
		if !foundAttempt {
			t.Errorf("event %d: gcp.storage.retry.attempt attribute missing", i)
		}
		if !foundInvID {
			t.Errorf("event %d: gcp.storage.gccl-invocation-id attribute missing", i)
		}
	}
}

func TestRecordRetryBackoffEventDevTracingDisabled(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "false")

	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})

	ctx, span := tracer().Start(ctx, "Bucket.Attrs")
	recordRetryBackoffEvent(ctx, 1, time.Now().Add(-100*time.Millisecond), "test-inv-id")
	span.End()

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if len(spans[0].Events) > 0 {
		t.Errorf("expected 0 events when dev tracing is disabled, got %d", len(spans[0].Events))
	}
}

func TestRunInvocationIDTracing(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	ctx, _ = startSpan(ctx, "Bucket.Attrs")
	attempts := 0
	err := run(ctx, func(c context.Context) error {
		attempts++
		if attempts == 1 {
			return &googleapi.Error{Code: 503}
		}
		return nil
	}, defaultRetry, true)

	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	endSpan(ctx, nil)

	spans := te.Spans()
	var parentSpan tracetest.SpanStub
	var backoffSpanCount int
	for _, s := range spans {
		if strings.HasSuffix(s.Name, "RetryBackoff") {
			backoffSpanCount++
		} else if strings.HasSuffix(s.Name, "Bucket.Attrs") {
			parentSpan = s
		}
	}

	if parentSpan.Name == "" {
		t.Fatalf("Bucket.Attrs parent span not found")
	}
	foundInvID := false
	for _, a := range parentSpan.Attributes {
		if string(a.Key) == "gcp.storage.gccl-invocation-id" {
			foundInvID = true
			if !strings.HasPrefix(a.Value.AsString(), "gccl-invocation-id/") {
				t.Errorf("gcp.storage.gccl-invocation-id = %q, want prefix gccl-invocation-id/", a.Value.AsString())
			}
		}
	}
	if !foundInvID {
		t.Errorf("gcp.storage.gccl-invocation-id attribute not found on parent span")
	}

	if backoffSpanCount != 1 {
		t.Errorf("expected 1 RetryBackoff child span, got %d", backoffSpanCount)
	}
	if len(parentSpan.Events) != 1 {
		t.Errorf("expected 1 retry event on parent span, got %d", len(parentSpan.Events))
	}
}

type staticTokenSource struct {
	token *oauth2.Token
	err   error
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	return s.token, s.err
}

func TestTracedTokenSourceSpan(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	rawTS := &staticTokenSource{token: &oauth2.Token{AccessToken: "fake-token"}}
	tts := NewTracedTokenSource(rawTS)

	tok, err := tts.Token()
	if err != nil {
		t.Fatalf("tts.Token: %v", err)
	}
	if tok.AccessToken != "fake-token" {
		t.Fatalf("got access token %q, want %q", tok.AccessToken, "fake-token")
	}

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got, want := spans[0].Name, "cloud.google.com/go/storage.Auth.RefreshAccessToken"; got != want {
		t.Errorf("got span name %q, want %q", got, want)
	}
}

func TestTracedTokenSourceNilBase(t *testing.T) {
	if got := NewTracedTokenSource(nil); got != nil {
		t.Errorf("NewTracedTokenSource(nil) = %v, want nil", got)
	}
}

func TestTracedTokenSourceError(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	testErr := fmt.Errorf("oauth2: token expired")
	rawTS := &staticTokenSource{err: testErr}
	tts := NewTracedTokenSource(rawTS)

	_, err := tts.Token()
	if err == nil {
		t.Fatalf("expected error from tts.Token, got nil")
	}

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != otcodes.Error {
		t.Errorf("got span status code %v, want %v", spans[0].Status.Code, otcodes.Error)
	}
}

func TestTracedTokenSourceDevTracingDisabled(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "false")

	rawTS := &staticTokenSource{token: &oauth2.Token{AccessToken: "fake-token"}}
	tts := NewTracedTokenSource(rawTS)

	tok, err := tts.Token()
	if err != nil {
		t.Fatalf("tts.Token: %v", err)
	}
	if tok.AccessToken != "fake-token" {
		t.Fatalf("got access token %q, want %q", tok.AccessToken, "fake-token")
	}

	spans := te.Spans()
	if len(spans) > 0 {
		t.Fatalf("expected 0 spans when dev tracing is disabled, got %d", len(spans))
	}
}

func TestStartChecksumSpan(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

	chkCtx, _ := startChecksumSpan(ctx, "CRC32C")
	endSpan(chkCtx, nil)

	spans := te.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	gotSpan := spans[0]
	if got, want := gotSpan.Name, "cloud.google.com/go/storage.Storage.CalculateChecksum"; got != want {
		t.Errorf("got span name %q, want %q", got, want)
	}
	foundChecksumType := false
	for _, a := range gotSpan.Attributes {
		if string(a.Key) == "gcp.storage.checksum.type" {
			foundChecksumType = true
			if got, want := a.Value.AsString(), "CRC32C"; got != want {
				t.Errorf("checksum type = %q, want %q", got, want)
			}
		}
	}
	if !foundChecksumType {
		t.Errorf("gcp.storage.checksum.type attribute not found on span")
	}
}

func TestChecksumSpanDevTracingDisabled(t *testing.T) {
	ctx := context.Background()
	te := testutil.NewOpenTelemetryTestExporter()
	t.Cleanup(func() {
		te.Unregister(ctx)
	})
	t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "false")

	chkCtx, span := startChecksumSpan(ctx, "CRC32C")
	endSpan(chkCtx, nil)

	if span.SpanContext().IsValid() {
		t.Errorf("expected invalid span context when dev tracing is disabled, got %v", span.SpanContext())
	}
	spans := te.Spans()
	if len(spans) > 0 {
		t.Fatalf("expected 0 ended spans because dev tracing is disabled, but got %d ended spans: %v", len(spans), spans[0].Name)
	}
}

func TestHTTPInternalWriterChecksumming(t *testing.T) {
	t.Run("single-shot emits checksum span", func(t *testing.T) {
		ctx := context.Background()
		te := testutil.NewOpenTelemetryTestExporter()
		t.Cleanup(func() {
			te.Unregister(ctx)
		})
		t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

		pr, pw := io.Pipe()
		defer pr.Close()
		serverChecksumChan := make(chan uint32, 1)
		close(serverChecksumChan)
		hiw := &httpInternalWriter{
			PipeWriter:         pw,
			ctx:                ctx,
			chunkSize:          0,
			serverChecksumChan: serverChecksumChan,
		}
		go io.ReadAll(pr)

		hiw.Write([]byte("some data"))
		hiw.Close()

		var foundChecksumSpan bool
		for _, s := range te.Spans() {
			if strings.HasSuffix(s.Name, "Storage.CalculateChecksum") {
				foundChecksumSpan = true
				foundChecksumType := false
				for _, a := range s.Attributes {
					if string(a.Key) == "gcp.storage.checksum.type" && a.Value.AsString() == "CRC32C" {
						foundChecksumType = true
					}
				}
				if !foundChecksumType {
					t.Errorf("gcp.storage.checksum.type attribute missing or incorrect on span")
				}
			}
		}
		if !foundChecksumSpan {
			t.Errorf("expected CalculateChecksum span for single-shot HTTP upload, got none")
		}
	})

	t.Run("chunked skips checksum span", func(t *testing.T) {
		ctx := context.Background()
		te := testutil.NewOpenTelemetryTestExporter()
		t.Cleanup(func() {
			te.Unregister(ctx)
		})
		t.Setenv("GO_STORAGE_DEV_OTEL_TRACING", "true")

		pr, pw := io.Pipe()
		defer pr.Close()
		serverChecksumChan := make(chan uint32, 1)
		close(serverChecksumChan)
		hiw := &httpInternalWriter{
			PipeWriter:         pw,
			ctx:                ctx,
			chunkSize:          8 * 1024 * 1024,
			serverChecksumChan: serverChecksumChan,
		}
		go io.ReadAll(pr)

		hiw.Write([]byte("some data"))
		hiw.Close()

		for _, s := range te.Spans() {
			if strings.HasSuffix(s.Name, "CalculateChecksum") {
				t.Errorf("expected no CalculateChecksum span for chunked HTTP upload, got %s", s.Name)
			}
		}
	})
}
