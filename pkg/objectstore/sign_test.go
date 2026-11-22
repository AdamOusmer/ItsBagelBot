// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore_test

import (
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/pkg/objectstore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignerMatchesPublishedS3Examples(t *testing.T) {
	signer := objectstore.Signer{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:          "us-east-1",
	}
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	cases := []struct {
		name      string
		target    string
		headers   map[string]string
		signed    string
		signature string
	}{
		{
			name:      "get object with range",
			target:    "https://examplebucket.s3.amazonaws.com/test.txt",
			headers:   map[string]string{"Range": "bytes=0-9"},
			signed:    "host;range;x-amz-content-sha256;x-amz-date",
			signature: "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
		},
		{
			name:      "get bucket lifecycle",
			target:    "https://examplebucket.s3.amazonaws.com/?lifecycle",
			signed:    "host;x-amz-content-sha256;x-amz-date",
			signature: "fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543",
		},
		{
			name:      "list objects",
			target:    "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J",
			signed:    "host;x-amz-content-sha256;x-amz-date",
			signature: "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.target, nil)
			require.NoError(t, err)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			signer.Sign(req, emptyHash, at)

			want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=" + tc.signed + ", Signature=" + tc.signature
			assert.Equal(t, want, req.Header.Get("Authorization"))
		})
	}
}
