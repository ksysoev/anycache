package inmemory

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		limit   uint
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
		setup   func(*testing.T) *InMemoryCacheStorage
		key     string
		want    string
		wantErr bool
	}{
		{
			name: "Successfully retrieve a value for an existing key",
			setup: func(t *testing.T) *InMemoryCacheStorage {
				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				s.Set(t.Context(), "key1", "value1", 0)

				return s
			},
			key:     "key1",
			want:    "value1",
			wantErr: false,
		},
		{
			name: "Fail to retrieve a value for a non-existing key",
			setup: func(t *testing.T) *InMemoryCacheStorage {
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
			setup: func(t *testing.T) *InMemoryCacheStorage {
				s, err := New(10)
				if err != nil {
					t.Fatalf("Failed to create InMemoryCacheStorage: %v", err)
				}

				s.Set(t.Context(), "key2", "value2", time.Microsecond)

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

			if tt.want != got {
				t.Errorf("Get() = %v, want %v", got, tt.want)
			}
		})
	}
}
