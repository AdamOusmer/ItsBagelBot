// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"ItsBagelBot/pkg/env"
)

var (
	ErrNotFound = errors.New("objectstore: object not found")
	ErrExists   = errors.New("objectstore: object already exists")
)

type Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	HTTP            *http.Client
}

func ConfigFromEnv(prefix string) Config {
	return Config{
		Endpoint:        env.MustGet(prefix + "ENDPOINT"),
		Bucket:          env.MustGet(prefix + "BUCKET"),
		AccessKeyID:     env.MustGet(prefix + "ACCESS_KEY_ID"),
		SecretAccessKey: env.MustGet(prefix + "SECRET_ACCESS_KEY"),
	}
}

type Object struct {
	Key          string
	Body         io.Reader
	Size         int64
	ContentType  string
	CacheControl string
}

type Info struct {
	Key          string
	Size         int64
	ContentType  string
	CacheControl string
	ETag         string
	LastModified time.Time
}

type Store interface {
	Put(ctx context.Context, obj Object) error
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	Head(ctx context.Context, key string) (Info, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
	Copy(ctx context.Context, src, dst string) error
}
