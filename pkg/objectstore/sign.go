// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	unsignedPayload = "UNSIGNED-PAYLOAD"
	unreserved      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
)

type Signer struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
}

// Every header already on req is signed, so set them all first.
func (s Signer) Sign(req *http.Request, payloadHash string, at time.Time) {
	amzDate := at.UTC().Format("20060102T150405Z")
	day := amzDate[:8]
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	names, block := canonicalHeaders(req)
	canonical := strings.Join([]string{
		req.Method,
		escapePath(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		block,
		names,
		payloadHash,
	}, "\n")
	scope := day + "/" + s.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))

	key := hmacSHA256([]byte("AWS4"+s.SecretAccessKey), day)
	for _, part := range []string{s.Region, "s3", "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.AccessKeyID, scope, names, hex.EncodeToString(hmacSHA256(key, toSign))))
}

func canonicalHeaders(req *http.Request) (names, block string) {
	values := map[string]string{"host": req.URL.Host}
	for name, v := range req.Header {
		values[strings.ToLower(name)] = strings.TrimSpace(strings.Join(v, ","))
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + ":" + values[k] + "\n")
	}
	return strings.Join(keys, ";"), b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, escape(k)+"="+escape(q.Get(k)))
	}
	return strings.Join(parts, "&")
}

func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = escape(seg)
	}
	return strings.Join(segments, "/")
}

func escape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
