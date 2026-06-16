package inmemory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		limit   int
		wantErr bool
	}{
		{
			name:    "Successfully create a new InMemoryCacheStorage with a valid limit",
			limit:   10,
			wantErr: false,
		},
		{
			name:    "Fail to create a new InMemoryCacheStorage with a limit of 0",
			limit:   0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := New(tt.limit)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("New() failed: %v", gotErr)
				}

				return
			}

			defer func() { _ = got.Close() }()

			if tt.wantErr {
				t.Fatal("New() succeeded unexpectedly")
			}

			if got == nil {
				t.Fatal("New() returned nil unexpectedly")
			}
		})
	}
}

func TestInMemoryCacheStorage_Get(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		setup   func(*testing.T) *Storage
		key     string
		want    string
		wantErr bool
	}{
		{
			name: "Successfully retrieve a value for an existing key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err := s.Set(t.Context(), "key1", []byte("value1"), 0); err != nil {
					t.Fatalf("Failed to set key1: %v", err)
				}

				return s
			},
			key:     "key1",
			want:    "value1",
			wantErr: false,
		},
		{
			name: "Fail to retrieve a value for a non-existing key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				return s
			},
			key:     "nonExistingKey",
			want:    "",
			wantErr: true,
		},
		{
			name: "Fail to retrieve a value for an expired key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err := s.Set(t.Context(), "key2", []byte("value2"), time.Microsecond); err != nil {
					t.Fatalf("Failed to set key2: %v", err)
				}

				time.Sleep(2 * time.Millisecond)

				return s
			},
			key:     "key2",
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.setup(t)

			defer func() { _ = s.Close() }()

			got, gotErr := s.Get(context.Background(), tt.key)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Get() failed: %v", gotErr)
				}

				return
			}

			if tt.wantErr {
				t.Fatal("Get() succeeded unexpectedly")
			}

			assert.Equal(t, tt.want, string(got), "Expected to get %v, but got '%v'", tt.want, string(got))
		})
	}
}

func TestInMemoryCacheStorage_Set(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		value         []byte
		ttl           time.Duration
		waitBeforeGet time.Duration
		wantErr       bool
		ableToGet     bool
	}{
		{
			name:          "Successfully set a value for a key with no TTL",
			key:           "key1",
			value:         []byte("value1"),
			wantErr:       false,
			waitBeforeGet: 0,
			ableToGet:     true,
		},
		{
			name:          "Successfully set a value for a key with a TTL and retrieve it before expiration",
			key:           "key2",
			value:         []byte("value2"),
			ttl:           2 * time.Second,
			wantErr:       false,
			waitBeforeGet: time.Millisecond,
			ableToGet:     true,
		},
		{
			name:          "Successfully set a value for a key with a TTL and fail to retrieve it after expiration",
			key:           "key3",
			value:         []byte("value3"),
			ttl:           time.Millisecond,
			wantErr:       false,
			waitBeforeGet: 2 * time.Millisecond,
			ableToGet:     false,
		},
		{
			name:          "Error when setting a value for a key with an invalid TTL",
			key:           "key4",
			value:         []byte("value4"),
			ttl:           -time.Second,
			wantErr:       true,
			waitBeforeGet: 0,
			ableToGet:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(10)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			defer func() { _ = s.Close() }()

			gotErr := s.Set(t.Context(), tt.key, tt.value, tt.ttl)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Set() failed: %v", gotErr)
				}

				return
			}

			if tt.wantErr {
				t.Fatal("Set() succeeded unexpectedly")
			}

			if tt.waitBeforeGet > 0 {
				time.Sleep(tt.waitBeforeGet)
			}

			value, err := s.Get(t.Context(), tt.key)

			if tt.ableToGet {
				assert.Equal(t, tt.value, value, "Expected to get %v, but got '%v'", tt.value, value)
			} else {
				assert.Error(t, err, "Expected to get an error, but got nil")
			}
		})
	}
}

func TestInMemoryCacheStorage_Del(t *testing.T) {
	tests := []struct {
		setup   func(*testing.T) *Storage
		name    string
		key     string
		want    bool
		wantErr bool
	}{
		{
			name: "Successfully delete an existing key",
			key:  "key1",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err := s.Set(t.Context(), "key1", []byte("value1"), 0); err != nil {
					t.Fatalf("Failed to set key1: %v", err)
				}

				return s
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "Fail to delete a non-existing key",
			key:  "key2",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err := s.Set(t.Context(), "key1", []byte("value1"), 0); err != nil {
					t.Fatalf("Failed to set key1: %v", err)
				}

				return s
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "Remove expired key",
			key:  "key3",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err := s.Set(t.Context(), "key3", []byte("value3"), time.Millisecond); err != nil {
					t.Fatalf("Failed to set key3: %v", err)
				}

				time.Sleep(2 * time.Millisecond)

				return s
			},
			want:    false,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.setup(t)

			defer func() { _ = s.Close() }()

			got, gotErr := s.Del(context.Background(), tt.key)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Del() failed: %v", gotErr)
				}

				return
			}

			if tt.wantErr {
				t.Fatal("Del() succeeded unexpectedly")
			}

			if tt.want != got {
				t.Errorf("Del() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInMemoryCacheStorage_GetWithTTL(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		setup   func(*testing.T) *Storage
		key     string
		want    string
		want2   time.Duration
		wantErr bool
	}{
		{
			name: "Successfully retrieve a value and TTL for an existing key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err = s.Set(t.Context(), "key1", []byte("value1"), time.Millisecond); err != nil {
					t.Fatalf("Failed to set key1: %v", err)
				}

				return s
			},
			key:     "key1",
			want:    "value1",
			want2:   time.Millisecond,
			wantErr: false,
		},
		{
			name: "Fail to retrieve a value and TTL for a non-existing key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err = s.Set(t.Context(), "key1", []byte("value1"), time.Millisecond); err != nil {
					t.Fatalf("Failed to set key1: %v", err)
				}

				return s
			},
			key:     "nonExistingKey",
			want:    "",
			want2:   0,
			wantErr: true,
		},
		{
			name: "Fail to retrieve a value and TTL for an expired key",
			setup: func(t *testing.T) *Storage {
				t.Helper()

				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				if err = s.Set(t.Context(), "key2", []byte("value2"), time.Millisecond); err != nil {
					t.Fatalf("Failed to set key2: %v", err)
				}

				time.Sleep(2 * time.Millisecond)

				return s
			},
			key: "key2", want: "",
			want2:   0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.setup(t)

			defer func() { _ = s.Close() }()

			got, got2, gotErr := s.GetWithTTL(context.Background(), tt.key)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("GetWithTTL() failed: %v", gotErr)
				}

				return
			}

			if tt.wantErr {
				t.Fatal("GetWithTTL() succeeded unexpectedly")
			}

			assert.Equal(t, tt.want, string(got), "Expected to get %v, but got '%v'", tt.want, string(got))

			if tt.want2 > 0 && got2 > 0 && tt.want2 < got2 {
				t.Errorf("GetWithTTL() = %v, want %v", got2, tt.want2)
			}

			if tt.want2 > 0 && got2 <= 0 {
				t.Errorf("GetWithTTL() = %v, want %v", got2, tt.want2)
			}
		})
	}
}

func TestInMemoryCacheStorage_Close(t *testing.T) {
	s, err := New(10)
	if err != nil {
		t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
	}

	err = s.Set(t.Context(), "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("Failed to set key1: %v", err)
	}

	_, err = s.Get(t.Context(), "key1")
	if err != nil {
		t.Fatalf("Failed to get key1: %v", err)
	}

	err = s.Close()
	if err != nil {
		t.Fatalf("Failed to close InMemoryCacheStorage: %v", err)
	}

	err = s.Set(t.Context(), "key2", []byte("value2"), 0)
	if err == nil {
		t.Fatal("Set() succeeded unexpectedly after Close()")
	}

	_, err = s.Get(t.Context(), "key1")
	if err == nil {
		t.Fatal("Get() succeeded unexpectedly after Close()")
	}

	err = s.Close()
	if err == nil {
		t.Fatal("Close() succeeded unexpectedly on already closed storage")
	}
}
